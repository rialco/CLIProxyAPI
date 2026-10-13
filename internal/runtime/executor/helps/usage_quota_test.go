package helps

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type usageQuotaRoundTripper func(*http.Request) (*http.Response, error)

func (f usageQuotaRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestUsageQuotaOnlyAllowsFixedReadOnlyEndpoints(t *testing.T) {
	for _, endpoint := range []string{"https://example.com/usage", "http://api.anthropic.com/api/oauth/usage", "https://api.anthropic.com/api/oauth/usage?token=x", "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if _, err := FetchUsageQuota(context.Background(), nil, nil, req); err == nil {
			t.Fatalf("allowed %s", endpoint)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/api/oauth/usage", nil)
	if _, err := FetchUsageQuota(context.Background(), nil, nil, req); err == nil {
		t.Fatal("allowed quota mutation")
	}
}

func TestUsageQuotaDoesNotFollowCredentialRedirects(t *testing.T) {
	calls := 0
	rt := usageQuotaRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://example.com/capture"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", rt)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	req.Header.Set("Authorization", "Bearer synthetic-test-token")
	if _, err := FetchUsageQuota(ctx, nil, nil, req); err == nil {
		t.Fatal("accepted redirect response")
	}
	if calls != 1 {
		t.Fatalf("followed credential redirect: %d calls", calls)
	}
}

func TestUsageQuotaReadsBoundedResponse(t *testing.T) {
	for _, size := range []int{10, (1 << 20) + 1} {
		rt := usageQuotaRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", size))), Request: req}, nil
		})
		ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", rt)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
		body, err := FetchUsageQuota(ctx, nil, nil, req)
		if size < 1<<20 && (err != nil || len(body) != size) {
			t.Fatalf("valid response: len=%d err=%v", len(body), err)
		}
		if size > 1<<20 && err == nil {
			t.Fatal("accepted unbounded response")
		}
	}
}
