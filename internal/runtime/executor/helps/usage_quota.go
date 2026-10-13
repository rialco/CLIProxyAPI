package helps

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// FetchUsageQuota only accepts the two read-only provider usage endpoints.
// Redirects are refused before another request can receive any credentials.
func FetchUsageQuota(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, req *http.Request) ([]byte, error) {
	if req == nil || req.URL == nil || req.Method != http.MethodGet || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" {
		return nil, fmt.Errorf("invalid usage quota request")
	}
	if req.URL.String() != "https://api.anthropic.com/api/oauth/usage" && req.URL.String() != "https://chatgpt.com/backend-api/wham/usage" {
		return nil, fmt.Errorf("unsupported usage quota endpoint")
	}
	client := *NewUtlsHTTPClient(ctx, cfg, auth, 0)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, fmt.Errorf("usage quota request failed")
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debug("usage quota response close failed")
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage quota returned HTTP %d", resp.StatusCode)
	}
	const maxBody = 1 << 20
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if errRead != nil || len(body) > maxBody {
		return nil, fmt.Errorf("invalid usage quota response")
	}
	return body, nil
}
