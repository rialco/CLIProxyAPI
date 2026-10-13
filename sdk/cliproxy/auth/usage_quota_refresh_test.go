package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestUsageQuotaRefreshBoundsAndBackoff(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	selector := &EarliestResetSelector{}
	schedule := &usageQuotaRefreshSchedule{}
	live := map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true}
	schedule.reconcile(selector, live)
	for _, id := range []string{"a", "b", "c", "d"} {
		if !schedule.begin(id, now) {
			t.Fatalf("could not start %s", id)
		}
	}
	if schedule.begin("e", now) || schedule.begin("a", now.Add(time.Hour)) {
		t.Fatal("concurrency cap or duplicate suppression failed")
	}
	schedule.finish("a", selector, true, now)
	if !schedule.begin("e", now) || schedule.begin("a", now) {
		t.Fatal("completed refresh did not free its slot or respect the polling interval")
	}
	if delay := schedule.states["a"].next.Sub(now); delay < time.Minute || delay > 75*time.Second {
		t.Fatalf("healthy refresh delay = %s", delay)
	}
	for _, id := range []string{"b", "c", "d", "e"} {
		schedule.finish(id, selector, true, now)
	}
	// The same schedule is independent of how many model requests arrive.
	for i := 0; i < 1000; i++ {
		if schedule.begin("a", now) {
			t.Fatal("extra refresh before the next polling deadline")
		}
	}
	for _, tc := range []struct {
		success bool
		delay   time.Duration
	}{{false, 2 * time.Minute}, {false, 4 * time.Minute}, {false, 4 * time.Minute}, {true, time.Minute}} {
		next := schedule.states["a"].next
		if schedule.begin("a", next.Add(-time.Nanosecond)) || !schedule.begin("a", next) {
			t.Fatal("incorrect polling boundary")
		}
		schedule.finish("a", selector, tc.success, next)
		delay := schedule.states["a"].next.Sub(next)
		if delay < tc.delay || delay > tc.delay+15*time.Second {
			t.Fatalf("refresh delay = %s, want %s plus at most 15s jitter", delay, tc.delay)
		}
	}
	// Removed accounts are pruned; strategy changes discard old retry schedules.
	delete(live, "a")
	schedule.reconcile(selector, live)
	if _, exists := schedule.states["a"]; exists {
		t.Fatal("removed account retained")
	}
	if !schedule.begin("b", now.Add(24*time.Hour)) {
		t.Fatal("could not start old strategy refresh")
	}
	newSelector := &EarliestResetSelector{}
	schedule.reconcile(newSelector, live)
	schedule.finish("b", selector, false, now)
	if !schedule.begin("b", now) {
		t.Fatal("old strategy refresh delayed the new strategy")
	}
}

func TestUsageQuotaRefreshDoesNotBlockRouting(t *testing.T) {
	now := time.Now()
	selector := &EarliestResetSelector{}
	m := NewManager(nil, selector, nil)
	a := &Auth{ID: "quota-nonblocking", Provider: "claude"}
	m.auths = map[string]*Auth{a.ID: a}
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(a.ID, "claude", []*registry.ModelInfo{{ID: "claude-sonnet"}})
	defer reg.UnregisterClient(a.ID)
	selector.setUsage(a, []UsageWindow{{ID: "five_hour", Period: 5 * time.Hour, ResetAt: now.Add(time.Hour)}}, now)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	probe := &quotaProbeExecutor{
		body:         []byte(fmt.Sprintf(`{"five_hour":{"utilization":10,"resets_at":%q}}`, now.Add(time.Hour).Format(time.RFC3339))),
		beforeReturn: func() { close(entered); <-release },
	}
	m.RegisterExecutor(probe)
	go func() {
		defer close(finished)
		m.refreshUsageQuota(context.Background(), selector, a, probe)
	}()
	defer func() { close(release); <-finished }()
	<-entered
	selected := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			got, err := m.SelectAuth(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{})
			if err != nil || got == nil || got.ID != a.ID {
				selected <- fmt.Errorf("cached routing failed: %v %v", got, err)
				return
			}
		}
		selected <- nil
	}()
	select {
	case err := <-selected:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("routing waited for the blocked usage request")
	}
}
