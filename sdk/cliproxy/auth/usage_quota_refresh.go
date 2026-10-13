package auth

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// UsageQuotaProvider performs a read-only usage query to its own fixed endpoint.
// It must never buy credits, reset allowances or execute a model request.
type UsageQuotaProvider interface {
	FetchUsageQuota(context.Context, *Auth) ([]byte, error)
}

const usageQuotaRefreshConcurrency = 4

type usageQuotaRefreshState struct {
	running  bool
	next     time.Time
	failures int
}

// usageQuotaRefreshSchedule is independent of request traffic. Network work
// never holds this lock or a manager/selector lock.
type usageQuotaRefreshSchedule struct {
	mu       sync.Mutex
	selector *EarliestResetSelector
	states   map[string]usageQuotaRefreshState
	inFlight int
}

func (s *usageQuotaRefreshSchedule) reconcile(selector *EarliestResetSelector, live map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = make(map[string]usageQuotaRefreshState)
	}
	for id, state := range s.states {
		if !state.running && !live[id] {
			delete(s.states, id)
		} else if s.selector != selector {
			state.next, state.failures = time.Time{}, 0
			s.states[id] = state
		}
	}
	s.selector = selector
}

func (s *usageQuotaRefreshSchedule) begin(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[id]
	if state.running || now.Before(state.next) || s.inFlight >= usageQuotaRefreshConcurrency {
		return false
	}
	state.running = true
	s.states[id] = state
	s.inFlight++
	return true
}

func (s *usageQuotaRefreshSchedule) finish(id string, selector *EarliestResetSelector, success bool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[id]
	state.running = false
	s.inFlight--
	if selector != s.selector {
		delete(s.states, id)
		return
	}
	state.failures = min(state.failures+1, 2)
	if success {
		state.failures = 0
	}
	// Stable per-account jitter spreads polling without a shared random source.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	delay := time.Minute * time.Duration(1<<state.failures)
	state.next = now.Add(delay + time.Duration(hash.Sum32()%16)*time.Second)
	s.states[id] = state
}

// StartUsageQuotaRefresh keeps earliest-reset routing independent of the UI.
// Other strategies make no usage requests. The service context owns the loop.
func (m *Manager) StartUsageQuotaRefresh(ctx context.Context) {
	if m == nil {
		return
	}
	ctx = nonNilContext(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		schedule := &usageQuotaRefreshSchedule{}
		for {
			selector, enabled := m.Selector().(*EarliestResetSelector)
			if enabled && !m.HomeEnabled() && ctx.Err() == nil {
				auths := m.List()
				live := make(map[string]bool, len(auths))
				for _, auth := range auths {
					live[auth.ID] = true
				}
				schedule.reconcile(selector, live)
				for _, auth := range auths {
					if auth.Disabled || auth.AuthKind() != AuthKindOAuth || (auth.Provider != "claude" && auth.Provider != "codex") {
						continue
					}
					executor, ok := m.Executor(auth.Provider)
					if !ok {
						continue
					}
					fetcher, ok := executor.(UsageQuotaProvider)
					if !ok {
						continue
					}
					if !schedule.begin(auth.ID, time.Now()) {
						continue
					}
					go func(auth *Auth, selector *EarliestResetSelector, fetcher UsageQuotaProvider) {
						success := false
						defer func() {
							schedule.finish(auth.ID, selector, success, time.Now())
						}()
						success = m.refreshUsageQuota(ctx, selector, auth, fetcher)
					}(auth, selector, fetcher)
				}
				selector.mu.Lock()
				for id := range selector.snapshots {
					if !live[id] {
						delete(selector.snapshots, id)
					}
				}
				selector.mu.Unlock()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (m *Manager) refreshUsageQuota(ctx context.Context, selector *EarliestResetSelector, auth *Auth, fetcher UsageQuotaProvider) bool {
	started := time.Now()
	body, errFetch := fetcher.FetchUsageQuota(ctx, auth)
	if errFetch != nil || ctx.Err() != nil {
		// Error responses and transport errors may contain credential material.
		log.WithField("provider", auth.Provider).Debug("usage quota refresh unavailable; keeping last observation")
		return false
	}
	windows := usageWindowsFromBody(auth.Provider, body, started)
	if len(windows) == 0 {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	current := m.auths[auth.ID]
	if m.selector != selector || current == nil || current.Disabled || current.RegistrationEpoch != auth.RegistrationEpoch || current.CredentialVersion != auth.CredentialVersion {
		return false
	}
	selector.setUsage(current, windows, started)
	return true
}
