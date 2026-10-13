package auth

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func usageNumber(value gjson.Result) (float64, bool) {
	if !value.Exists() || (value.Type != gjson.Number && value.Type != gjson.String) {
		return 0, false
	}
	number, err := strconv.ParseFloat(value.String(), 64)
	return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0
}

func usageReset(value gjson.Result, observedAt time.Time, after gjson.Result) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, value.String()); err == nil {
		return parsed
	}
	if seconds, ok := usageNumber(value); ok && seconds > 0 && seconds < 1e11 {
		return time.Unix(int64(seconds), 0)
	}
	if seconds, ok := usageNumber(after); ok && seconds > 0 && seconds <= 366*24*60*60 {
		return observedAt.Add(time.Duration(seconds * float64(time.Second)))
	}
	return time.Time{}
}

func usageWindowsFromBody(provider string, body []byte, now time.Time) []UsageWindow {
	if !gjson.ValidBytes(body) {
		return nil
	}
	root := gjson.ParseBytes(body)
	var windows []UsageWindow
	add := func(id, model string, used gjson.Result, period time.Duration, reset time.Time) {
		percent, ok := usageNumber(used)
		if !ok || period <= 0 {
			return
		}
		if reset.IsZero() {
			if percent >= 100 {
				windows = append(windows, UsageWindow{ID: id, Model: model, UnavailableUntil: now.Add(usageQuotaMaxAge)})
			}
			return
		}
		windows = append(windows, UsageWindow{ID: id, Model: model, UsedPercent: math.Min(percent, 100), Period: period, ResetAt: reset})
	}
	switch strings.ToLower(provider) {
	case "claude":
		for _, entry := range []struct {
			key, model string
			period     time.Duration
		}{
			{"five_hour", "", 5 * time.Hour}, {"seven_day", "", 7 * 24 * time.Hour},
			{"seven_day_oauth_apps", "", 7 * 24 * time.Hour},
			{"seven_day_opus", "opus", 7 * 24 * time.Hour}, {"seven_day_sonnet", "sonnet", 7 * 24 * time.Hour},
		} {
			window := root.Get(entry.key)
			add(entry.key, entry.model, window.Get("utilization"), entry.period, usageReset(window.Get("resets_at"), now, gjson.Result{}))
		}
		for _, limit := range root.Get("limits").Array() {
			if limit.Get("kind").String() != "weekly_scoped" || limit.Get("is_active").Type == gjson.False {
				continue
			}
			model := limit.Get("scope.model.id").String()
			if model == "" {
				model = limit.Get("scope.model.display_name").String()
			}
			if model != "" {
				add("scoped-"+model, model, limit.Get("percent"), 7*24*time.Hour, usageReset(limit.Get("resets_at"), now, gjson.Result{}))
			}
		}
	case "codex":
		addLimits := func(limit gjson.Result, model, prefix string) {
			if limit.Get("allowed").Type == gjson.False || limit.Get("limit_reached").Bool() {
				windows = append(windows, UsageWindow{ID: prefix + "unavailable", Model: model, UnavailableUntil: now.Add(usageQuotaMaxAge)})
			}
			for _, key := range []string{"primary_window", "secondary_window"} {
				window := limit.Get(key)
				seconds, valid := usageNumber(window.Get("limit_window_seconds"))
				if !valid || seconds > 366*24*60*60 {
					continue
				}
				add(prefix+key, model, window.Get("used_percent"), time.Duration(seconds*float64(time.Second)), usageReset(window.Get("reset_at"), now, window.Get("reset_after_seconds")))
			}
		}
		addLimits(root.Get("rate_limit"), "", "")
		for _, limit := range root.Get("additional_rate_limits").Array() {
			model := limit.Get("limit_name").String()
			if model != "" {
				addLimits(limit.Get("rate_limit"), model, model+"-")
			}
		}
	}
	return windows
}

func usageWindowsFromSignals(provider string, quota QuotaState) []UsageWindow {
	// Translate only the measured, included windows. Billing credits, paid
	// overage, request counters and unrelated model pools do not imply capacity.
	get := func(key string) string { return quota.Signals[http.CanonicalHeaderKey(key)] }
	var windows []UsageWindow
	add := func(id, used, reset, after string, period time.Duration, multiplier float64) {
		percent, err := strconv.ParseFloat(used, 64)
		if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || period <= 0 {
			return
		}
		parseScalar := func(s string) gjson.Result { return gjson.Result{Type: gjson.String, Str: s} }
		resetAt := usageReset(parseScalar(reset), quota.ObservedAt, parseScalar(after))
		if resetAt.IsZero() {
			return
		}
		windows = append(windows, UsageWindow{ID: id, UsedPercent: math.Min(percent*multiplier, 100), ResetAt: resetAt, Period: period})
	}
	switch strings.ToLower(provider) {
	case "claude":
		for _, entry := range []struct {
			id, prefix string
			period     time.Duration
		}{
			{"five_hour", "5h", 5 * time.Hour}, {"seven_day", "7d", 7 * 24 * time.Hour},
		} {
			prefix := "Anthropic-Ratelimit-Unified-" + entry.prefix + "-"
			used := get(prefix + "Utilization")
			if strings.EqualFold(get(prefix+"Status"), "rejected") {
				used = "1"
			}
			add(entry.id, used, get(prefix+"Reset"), "", entry.period, 100)
		}
	case "codex":
		for _, key := range []string{"primary", "secondary"} {
			prefix := "X-Codex-" + key + "-"
			minutes, err := strconv.ParseFloat(get(prefix+"Window-Minutes"), 64)
			if err != nil || math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes > 366*24*60 {
				continue
			}
			add(key+"_window", get(prefix+"Used-Percent"), get(prefix+"Reset-At"), get(prefix+"Reset-After-Seconds"), time.Duration(minutes*float64(time.Minute)), 1)
		}
	}
	return windows
}
