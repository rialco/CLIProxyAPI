package auth

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestEarliestResetConsidersBothWindows(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, provider := range []string{"claude", "codex"} {
		for _, tc := range []struct {
			name                                             string
			aShort, aWeekly, bShort, bWeekly                 time.Duration
			aShortUsed, aWeeklyUsed, bShortUsed, bWeeklyUsed float64
			want                                             string
		}{
			{name: "short window outranks earlier week", aShort: 4 * time.Hour, aWeekly: 24 * time.Hour, bShort: time.Hour, bWeekly: 5 * 24 * time.Hour, aWeeklyUsed: 50, bWeeklyUsed: 20, want: "b"},
			{name: "weekly window outranks earlier short window", aShort: 4 * time.Hour, aWeekly: 30 * time.Minute, bShort: time.Hour, bWeekly: 5 * 24 * time.Hour, want: "a"},
			{name: "short limit exhausted", aShort: 10 * time.Minute, aWeekly: 20 * time.Minute, bShort: time.Hour, bWeekly: 5 * 24 * time.Hour, aShortUsed: 100, want: "b"},
			{name: "weekly limit exhausted", aShort: 10 * time.Minute, aWeekly: 20 * time.Minute, bShort: time.Hour, bWeekly: 5 * 24 * time.Hour, aWeeklyUsed: 100, want: "b"},
			{name: "each account has a different exhausted limit", aShort: time.Hour, aWeekly: 24 * time.Hour, bShort: 2 * time.Hour, bWeekly: 5 * 24 * time.Hour, aShortUsed: 100, bWeeklyUsed: 100},
			{name: "passed short reset no longer ranks", aShort: -time.Minute, aWeekly: 24 * time.Hour, bShort: time.Hour, bWeekly: 5 * 24 * time.Hour, aShortUsed: 100, want: "b"},
			{name: "stable tie", aShort: time.Hour, aWeekly: 24 * time.Hour, bShort: time.Hour, bWeekly: 5 * 24 * time.Hour, want: "a"},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				selector := &EarliestResetSelector{nowFunc: func() time.Time { return now }}
				a, b := &Auth{ID: "a", Provider: provider}, &Auth{ID: "b", Provider: provider}
				selector.setUsage(a, []UsageWindow{
					{ID: "short", UsedPercent: tc.aShortUsed, Period: 5 * time.Hour, ResetAt: now.Add(tc.aShort)},
					{ID: "weekly", UsedPercent: tc.aWeeklyUsed, Period: 7 * 24 * time.Hour, ResetAt: now.Add(tc.aWeekly)},
				}, now)
				selector.setUsage(b, []UsageWindow{
					{ID: "short", UsedPercent: tc.bShortUsed, Period: 5 * time.Hour, ResetAt: now.Add(tc.bShort)},
					{ID: "weekly", UsedPercent: tc.bWeeklyUsed, Period: 7 * 24 * time.Hour, ResetAt: now.Add(tc.bWeekly)},
				}, now)
				for i := 0; i < 10; i++ {
					got, err := selector.Pick(context.Background(), provider, "model", cliproxyexecutor.Options{}, []*Auth{b, a})
					if tc.want == "" {
						if err == nil || got != nil {
							t.Fatalf("exhausted accounts remained eligible: %v %v", got, err)
						}
					} else if err != nil || got == nil || got.ID != tc.want {
						t.Fatalf("pick = %v, %v; want %s", got, err, tc.want)
					}
				}
			})
		}
	}
}

func TestEarliestResetResponseHeadersBlockEitherWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, window := range []string{"5h", "7d"} {
		t.Run(window, func(t *testing.T) {
			selector := &EarliestResetSelector{nowFunc: func() time.Time { return now }}
			a, b := &Auth{ID: "a", Provider: "claude"}, &Auth{ID: "b", Provider: "claude"}
			selector.setUsage(a, []UsageWindow{
				{ID: "five_hour", UsedPercent: 10, Period: 5 * time.Hour, ResetAt: now.Add(time.Hour)},
				{ID: "seven_day", UsedPercent: 50, Period: 7 * 24 * time.Hour, ResetAt: now.Add(24 * time.Hour)},
			}, now)
			selector.setUsage(b, []UsageWindow{{ID: "five_hour", Period: 5 * time.Hour, ResetAt: now.Add(2 * time.Hour)}}, now)
			got, err := selector.Pick(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{}, []*Auth{a, b})
			if err != nil || got == nil || got.ID != "a" {
				t.Fatalf("initial pick = %v %v", got, err)
			}
			headers := make(http.Header)
			headers.Set("Anthropic-Ratelimit-Unified-"+window+"-Utilization", "1")
			headers.Set("Anthropic-Ratelimit-Unified-"+window+"-Reset", strconv.FormatInt(now.Add(4*time.Hour).Unix(), 10))
			a.Quota.ObserveResponseHeadersForProvider("claude", headers, now)
			got, err = selector.Pick(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{}, []*Auth{a, b})
			if err != nil || got == nil || got.ID != "b" {
				t.Fatalf("exhausted window not skipped: %v %v", got, err)
			}
		})
	}
}

func BenchmarkEarliestResetCachedSelection(b *testing.B) {
	for _, count := range []int{4, 20, 100} {
		b.Run(fmt.Sprintf("accounts-%d", count), func(b *testing.B) {
			now := time.Now()
			selector := &EarliestResetSelector{nowFunc: func() time.Time { return now }}
			auths := make([]*Auth, count)
			for i := range auths {
				auths[i] = &Auth{ID: fmt.Sprintf("account-%03d", i), Provider: "claude"}
				selector.setUsage(auths[i], []UsageWindow{
					{ID: "five_hour", UsedPercent: 10, Period: 5 * time.Hour, ResetAt: now.Add(time.Duration(i+1) * time.Minute)},
					{ID: "seven_day", UsedPercent: 50, Period: 7 * 24 * time.Hour, ResetAt: now.Add(24 * time.Hour)},
				}, now)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := selector.Pick(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{}, auths); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestEarliestResetFreshnessAndReplacement(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	selector := &EarliestResetSelector{nowFunc: func() time.Time { return now }}
	a := &Auth{ID: "a", Provider: "codex", RegistrationEpoch: 1, CredentialVersion: 1}
	b := &Auth{ID: "b", Provider: "codex"}
	window := UsageWindow{ID: "secondary_window", UsedPercent: 25, Period: 7 * 24 * time.Hour, ResetAt: now.Add(time.Hour)}
	selector.setUsage(a, []UsageWindow{window}, now)
	if reset, _ := selector.profile(a, "gpt", now); reset.IsZero() {
		t.Fatal("fresh snapshot missing")
	}
	a.CredentialVersion++
	if reset, _ := selector.profile(a, "gpt", now); !reset.IsZero() {
		t.Fatal("snapshot survived credential replacement")
	}
	a.CredentialVersion--
	now = now.Add(usageQuotaMaxAge + time.Second)
	for _, want := range []string{"a", "b", "a", "b"} {
		got, err := selector.Pick(context.Background(), "codex", "gpt", cliproxyexecutor.Options{}, []*Auth{a, b})
		if err != nil || got.ID != want {
			t.Fatalf("stale quota fallback: got %v %v, want %s", got, err, want)
		}
	}
	// Once the reported reset passes, do not reuse its old exhaustion flag.
	window.UsedPercent = 100
	window.ResetAt = now
	selector.setUsage(a, []UsageWindow{window}, now)
	if reset, blocked := selector.profile(a, "gpt", now); !reset.IsZero() || !blocked.IsZero() {
		t.Fatal("expired window survived reset")
	}
}

func TestEarliestResetKeepsKnownExhaustionDuringRefreshBackoff(t *testing.T) {
	observedAt := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	now := observedAt.Add(usageQuotaMaxAge + time.Minute)
	selector := &EarliestResetSelector{nowFunc: func() time.Time { return now }}
	a := &Auth{ID: "a", Provider: "claude"}
	resetAt := observedAt.Add(time.Hour)
	selector.setUsage(a, []UsageWindow{
		{ID: "five_hour", UsedPercent: 100, Period: 5 * time.Hour, ResetAt: resetAt},
		{ID: "seven_day", UsedPercent: 50, Period: 7 * 24 * time.Hour, ResetAt: observedAt.Add(24 * time.Hour)},
	}, observedAt)
	if _, blocked := selector.profile(a, "claude-sonnet", now); !blocked.Equal(resetAt) {
		t.Fatal("refresh failure released an exhausted account before its reset")
	}
	if reset, blocked := selector.profile(a, "claude-sonnet", resetAt); !reset.IsZero() || !blocked.IsZero() {
		t.Fatal("expired estimates survived the exhausted window's reset")
	}
	// A newer response can explicitly clear the exhausted state.
	a.Quota.ObserveResponseHeadersForProvider("claude", http.Header{
		"Anthropic-Ratelimit-Unified-5h-Utilization": {"0.2"},
		"Anthropic-Ratelimit-Unified-5h-Reset":       {strconv.FormatInt(resetAt.Unix(), 10)},
	}, observedAt.Add(time.Second))
	if reset, blocked := selector.profile(a, "claude-sonnet", now); !reset.IsZero() || !blocked.IsZero() {
		t.Fatal("newer stale recovery should clear exhaustion without claiming fresh capacity")
	}
	a.Quota.ObserveResponseHeadersForProvider("claude", http.Header{
		"Anthropic-Ratelimit-Unified-5h-Utilization": {"1"},
		"Anthropic-Ratelimit-Unified-5h-Reset":       {strconv.FormatInt(resetAt.Unix(), 10)},
	}, observedAt.Add(2*time.Second))
	if _, blocked := selector.profile(a, "claude-sonnet", now); !blocked.Equal(resetAt) {
		t.Fatal("response-header exhaustion was lost during refresh backoff")
	}
	// Successful polling with current capacity replaces old exhaustion, too.
	a.Quota = QuotaState{}
	selector.setUsage(a, []UsageWindow{{ID: "five_hour", UsedPercent: 20, Period: 5 * time.Hour, ResetAt: resetAt}}, now)
	if reset, blocked := selector.profile(a, "claude-sonnet", now); !reset.Equal(resetAt) || !blocked.IsZero() {
		t.Fatal("fresh capacity did not replace exhaustion")
	}
}

func TestEarliestResetManagerAliasesPriorityAndFailover(t *testing.T) {
	now := time.Now()
	selector := &EarliestResetSelector{}
	manager := NewManager(nil, selector, nil)
	manager.RegisterExecutor(&quotaProbeExecutor{})
	a := &Auth{ID: "reset-priority-a", Provider: "claude", Attributes: map[string]string{"priority": "10", AttributeAuthKind: AuthKindOAuth}}
	b := &Auth{ID: "reset-priority-b", Provider: "claude", Attributes: map[string]string{AttributeAuthKind: AuthKindOAuth}}
	manager.auths = map[string]*Auth{a.ID: a, b.ID: b}
	reg := registry.GetGlobalRegistry()
	for _, auth := range []*Auth{a, b} {
		reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{{ID: "claude-sonnet"}})
		defer reg.UnregisterClient(auth.ID)
	}
	selector.setUsage(a, []UsageWindow{{ID: "seven_day", UsedPercent: 100, Period: 7 * 24 * time.Hour, ResetAt: now.Add(time.Hour)}}, now)
	selector.setUsage(b, []UsageWindow{{ID: "seven_day", UsedPercent: 20, Period: 7 * 24 * time.Hour, ResetAt: now.Add(5 * 24 * time.Hour)}}, now)
	if manager.useSchedulerFastPath() {
		t.Fatal("cached scheduler would bypass quota selection")
	}
	got, err := manager.SelectAuth(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{})
	if err != nil || got.ID != b.ID {
		t.Fatalf("exhausted high-priority account blocked fallback: %v %v", got, err)
	}
	selector.setUsage(a, []UsageWindow{{ID: "seven_day", UsedPercent: 20, Period: 7 * 24 * time.Hour, ResetAt: now.Add(6 * 24 * time.Hour)}}, now)
	got, err = manager.SelectAuth(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{})
	if err != nil || got.ID != a.ID {
		t.Fatalf("manual priority was ignored: %v %v", got, err)
	}
	got, _, err = manager.pickNextLegacy(context.Background(), "claude", "claude-sonnet", cliproxyexecutor.Options{}, map[string]struct{}{a.ID: {}})
	if err != nil || got.ID != b.ID {
		t.Fatalf("tried account was reused: %v %v", got, err)
	}
	got, _, _, err = manager.pickNextMixedLegacy(context.Background(), []string{"claude"}, "claude-sonnet", cliproxyexecutor.Options{}, map[string]struct{}{a.ID: {}})
	if err != nil || got.ID != b.ID {
		t.Fatalf("mixed-provider failover bypassed quota selection: %v %v", got, err)
	}
	manager.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{
		"claude": {{Name: "claude-opus", Alias: "friendly-model", Fork: true}},
	})
	for _, auth := range []*Auth{a, b} {
		reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{{ID: "friendly-model"}, {ID: "claude-opus"}})
	}
	selector.setUsage(a, []UsageWindow{{ID: "seven_day_opus", Model: "opus", UsedPercent: 100, Period: 7 * 24 * time.Hour, ResetAt: now.Add(time.Hour)}}, now)
	got, err = manager.SelectAuth(context.Background(), "claude", "friendly-model", cliproxyexecutor.Options{})
	if err != nil || got.ID != b.ID {
		t.Fatalf("model alias hid exhausted Opus window: %v %v", got, err)
	}
}

type quotaProbeExecutor struct {
	ProviderExecutor
	body         []byte
	beforeReturn func()
}

func (*quotaProbeExecutor) Identifier() string { return "claude" }
func (e *quotaProbeExecutor) FetchUsageQuota(context.Context, *Auth) ([]byte, error) {
	if e.beforeReturn != nil {
		e.beforeReturn()
	}
	return e.body, nil
}

func TestUsageQuotaRefreshRejectsStaleCredentialOrSelector(t *testing.T) {
	for _, kind := range []string{"valid", "replacement", "removed", "strategy change", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			selector := &EarliestResetSelector{}
			m := NewManager(nil, selector, nil)
			a := &Auth{ID: "a", Provider: "claude", RegistrationEpoch: 1, CredentialVersion: 1}
			m.auths = map[string]*Auth{"a": a.Clone()}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &quotaProbeExecutor{body: []byte(fmt.Sprintf(`{"seven_day":{"utilization":50,"resets_at":%q}}`, time.Now().Add(24*time.Hour).Format(time.RFC3339)))}
			probe.beforeReturn = func() {
				switch kind {
				case "replacement":
					m.auths["a"].CredentialVersion++
				case "removed":
					delete(m.auths, "a")
				case "strategy change":
					m.selector = &RoundRobinSelector{}
				case "cancelled":
					cancel()
				}
			}
			m.refreshUsageQuota(ctx, selector, a, probe)
			if (len(selector.snapshots) > 0) != (kind == "valid") {
				t.Fatalf("unexpected snapshot for %s", kind)
			}
		})
	}
}

func TestUsageWindowsParsingAndScope(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	claude := usageWindowsFromBody("claude", []byte(`{"five_hour":{"utilization":10,"resets_at":"2026-10-12T16:00:00Z"},"seven_day":{"utilization":50,"resets_at":"2026-10-13T12:00:00Z"},"seven_day_opus":{"utilization":100,"resets_at":"2026-10-14T12:00:00Z"},"extra_usage":{"is_enabled":true,"utilization":0}}`), now)
	if len(claude) != 3 {
		t.Fatalf("Claude windows: %v", claude)
	}
	s := &EarliestResetSelector{}
	a := &Auth{ID: "a", Provider: "claude"}
	s.setUsage(a, claude, now)
	if _, blocked := s.profile(a, "claude-sonnet", now); !blocked.IsZero() {
		t.Fatal("Opus limit blocked Sonnet")
	}
	if _, blocked := s.profile(a, "claude-opus", now); blocked.IsZero() {
		t.Fatal("Opus exhaustion ignored")
	}
	codex := usageWindowsFromBody("codex", []byte(`{"rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":18000,"reset_after_seconds":3600},"secondary_window":{"used_percent":50,"limit_window_seconds":604800,"reset_at":1791892800}},"code_review_rate_limit":{"primary_window":{"used_percent":100,"limit_window_seconds":18000,"reset_after_seconds":100}},"credits":{"unlimited":true}}`), now)
	if len(codex) != 2 || !codex[0].ResetAt.Equal(now.Add(time.Hour)) || codex[1].Period != 7*24*time.Hour {
		t.Fatalf("Codex windows: %v", codex)
	}
	for _, body := range []string{`{"rate_limit":{"allowed":false}}`, `{"rate_limit":{"limit_reached":true}}`} {
		windows := usageWindowsFromBody("codex", []byte(body), now)
		if len(windows) != 1 || !windows[0].ResetAt.IsZero() || !windows[0].UnavailableUntil.After(now) {
			t.Fatalf("explicit unavailable state lost: %v", windows)
		}
	}
	for _, bad := range []string{`not JSON`, `{"five_hour":{"utilization":"NaN","resets_at":"2026-10-13T12:00:00Z"}}`, `{"five_hour":{"utilization":-1,"resets_at":"2026-10-13T12:00:00Z"}}`, `{"five_hour":{"utilization":10,"resets_at":null}}`} {
		if got := usageWindowsFromBody("claude", []byte(bad), now); len(got) != 0 {
			t.Fatalf("malformed quota accepted: %v", got)
		}
	}
}
