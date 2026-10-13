package executor

import (
	"context"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (e *ClaudeExecutor) FetchUsageQuota(ctx context.Context, auth *cliproxyauth.Auth) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-cli/2.1.280 (external, cli)")
	if errPrepare := e.PrepareRequest(req, auth); errPrepare != nil {
		return nil, errPrepare
	}
	return helps.FetchUsageQuota(ctx, e.cfg, auth, req)
}

func (e *CodexExecutor) FetchUsageQuota(ctx context.Context, auth *cliproxyauth.Auth) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, err
	}
	if auth != nil && auth.Metadata != nil {
		if accountID, ok := auth.Metadata["account_id"].(string); ok && accountID != "" {
			req.Header.Set("Chatgpt-Account-Id", accountID)
		}
	}
	if errPrepare := e.PrepareRequest(req, auth); errPrepare != nil {
		return nil, errPrepare
	}
	return helps.FetchUsageQuota(ctx, e.cfg, auth, req)
}

func (e *CodexAutoExecutor) FetchUsageQuota(ctx context.Context, auth *cliproxyauth.Auth) ([]byte, error) {
	if e == nil || e.httpExec == nil {
		return nil, fmt.Errorf("codex executor unavailable")
	}
	return e.httpExec.FetchUsageQuota(ctx, auth)
}
