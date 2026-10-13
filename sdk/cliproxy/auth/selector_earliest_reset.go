package auth

import (
	"context"
	"strings"
	"sync"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

const usageQuotaMaxAge = 5 * time.Minute

// UsageWindow describes an included allowance, never paid overage or reset credits.
type UsageWindow struct {
	ID          string
	Model       string
	UsedPercent float64
	Period      time.Duration
	ResetAt     time.Time
	// UnavailableUntil is a short observation hold when upstream explicitly
	// denies capacity without identifying the exhausted window. It is not a reset.
	UnavailableUntil time.Time
}

type usageQuotaSnapshot struct {
	windows           []UsageWindow
	observedAt        time.Time
	registrationEpoch uint64
	credentialVersion uint64
}

// EarliestResetSelector drains the available allowance with the earliest reset
// across all applicable usage windows. Exhausting any window blocks the account.
// It deliberately uses the manager's route-aware selection path, not the cached
// round-robin scheduler, because quota availability changes independently.
type EarliestResetSelector struct {
	mu        sync.RWMutex
	snapshots map[string]usageQuotaSnapshot
	fallback  RoundRobinSelector
	nowFunc   func() time.Time
}

func (s *EarliestResetSelector) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func (s *EarliestResetSelector) setUsage(auth *Auth, windows []UsageWindow, observedAt time.Time) {
	if auth == nil || len(windows) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshots == nil {
		s.snapshots = make(map[string]usageQuotaSnapshot)
	}
	s.snapshots[auth.ID] = usageQuotaSnapshot{
		windows: append([]UsageWindow(nil), windows...), observedAt: observedAt,
		registrationEpoch: auth.RegistrationEpoch, credentialVersion: auth.CredentialVersion,
	}
}

func freshUsageObservation(observedAt, now time.Time) bool {
	return !observedAt.IsZero() && !observedAt.After(now) && now.Sub(observedAt) <= usageQuotaMaxAge
}

func keepUsageWindow(window UsageWindow, observedAt, now time.Time) bool {
	if observedAt.IsZero() || observedAt.After(now) {
		return false
	}
	// A failed refresh cannot make a known exhausted allowance usable before
	// its reported reset. Positive capacity estimates still expire normally.
	return freshUsageObservation(observedAt, now) ||
		(window.UsedPercent >= 100 && window.ResetAt.After(now)) ||
		window.UnavailableUntil.After(now)
}

func (s *EarliestResetSelector) windows(auth *Auth, now time.Time) []UsageWindow {
	if auth == nil {
		return nil
	}
	s.mu.RLock()
	snapshot := s.snapshots[auth.ID]
	s.mu.RUnlock()
	var windows []UsageWindow
	validSnapshot := snapshot.registrationEpoch == auth.RegistrationEpoch && snapshot.credentialVersion == auth.CredentialVersion
	if validSnapshot {
		for _, window := range snapshot.windows {
			if keepUsageWindow(window, snapshot.observedAt, now) {
				windows = append(windows, window)
			}
		}
	}
	// Response headers make active requests immediately visible, while a partial
	// response must not erase other windows returned by the periodic usage probe.
	if !auth.Quota.ObservedAt.IsZero() && !auth.Quota.ObservedAt.After(now) && (!validSnapshot || !auth.Quota.ObservedAt.Before(snapshot.observedAt)) {
		for _, window := range usageWindowsFromSignals(auth.Provider, auth.Quota) {
			keep := keepUsageWindow(window, auth.Quota.ObservedAt, now)
			replaced := false
			for i := range windows {
				if windows[i].ID == window.ID {
					if keep {
						windows[i] = window
					} else {
						// A newer recovery observation supersedes old exhaustion,
						// even when that recovery estimate has itself expired.
						windows = append(windows[:i], windows[i+1:]...)
					}
					replaced = true
					break
				}
			}
			if !replaced && keep {
				windows = append(windows, window)
			}
		}
	}
	return windows
}

func usageWindowMatchesModel(window UsageWindow, model string) bool {
	if window.Model == "" {
		return true
	}
	normalize := func(s string) string {
		return strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(strings.ToLower(s))
	}
	return strings.Contains(normalize(model), normalize(window.Model))
}

// profile returns the prioritization deadline and the latest blocking reset.
// Unknown or expired observations are not interpreted as fresh free quota.
func (s *EarliestResetSelector) profile(auth *Auth, model string, now time.Time) (resetAt, blockedUntil time.Time) {
	for _, window := range s.windows(auth, now) {
		if !usageWindowMatchesModel(window, model) {
			continue
		}
		if window.UnavailableUntil.After(now) && window.UnavailableUntil.After(blockedUntil) {
			blockedUntil = window.UnavailableUntil
		}
		if !window.ResetAt.After(now) {
			continue
		}
		if window.UsedPercent >= 100 && window.ResetAt.After(blockedUntil) {
			blockedUntil = window.ResetAt
		}
		if resetAt.IsZero() || window.ResetAt.Before(resetAt) {
			resetAt = window.ResetAt
		}
	}
	return resetAt, blockedUntil
}

func (s *EarliestResetSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	now := s.now()
	candidates := make([]*Auth, 0, len(auths))
	{
		var earliest time.Time
		for _, auth := range auths {
			_, blockedUntil := s.profile(auth, usageQuotaModel(ctx, auth, model), now)
			if blockedUntil.IsZero() {
				candidates = append(candidates, auth)
			} else if earliest.IsZero() || blockedUntil.Before(earliest) {
				earliest = blockedUntil
			}
		}
		if len(candidates) == 0 && !earliest.IsZero() {
			return nil, newModelCooldownError(model, provider, earliest.Sub(now))
		}
	}
	available, err := getSelectorAvailableAuths(ctx, candidates, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)
	var selected *Auth
	var soonest time.Time
	for _, auth := range available {
		// The manager supplies the resolved model in a private per-request context
		// so aliases cannot hide a model-specific allowance.
		quotaModel := usageQuotaModel(ctx, auth, model)
		resetAt, _ := s.profile(auth, quotaModel, now)
		if resetAt.IsZero() {
			continue
		}
		if selected == nil || resetAt.Before(soonest) || (resetAt.Equal(soonest) && auth.ID < selected.ID) {
			selected, soonest = auth, resetAt
		}
	}
	if selected != nil {
		return selected, nil
	}
	// No reliable reset dates: continue normal balanced routing. Background
	// probes include unused accounts, so unknown accounts cannot stay undiscovered.
	return s.fallback.Pick(context.WithValue(nonNilContext(ctx), prevalidatedAuthCandidatesKey{}, true), provider, model, opts, available)
}

type usageQuotaModelsKey struct{}

func usageQuotaModel(ctx context.Context, auth *Auth, fallback string) string {
	if models, ok := ctxValue(ctx, usageQuotaModelsKey{}).(map[string]string); ok && auth != nil {
		if model, exists := models[auth.ID]; exists {
			return model
		}
	}
	return fallback
}

func (m *Manager) withUsageQuotaModels(ctx context.Context, selector Selector, auths []*Auth, model string) context.Context {
	if _, ok := selector.(*EarliestResetSelector); !ok {
		return ctx
	}
	models := make(map[string]string, len(auths))
	for _, auth := range auths {
		models[auth.ID] = m.selectionModelForAuth(auth, model)
	}
	return context.WithValue(nonNilContext(ctx), usageQuotaModelsKey{}, models)
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ctxValue(ctx context.Context, key any) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(key)
}
