package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestATIDRemovedExtensionsAndCoreManagement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "config-version: 8\nserver: {port: 8318}\nmanagement: {secret-key: test-password}\nplugins: {enabled: true, dir: unused}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthDir = t.TempDir()
	h := management.NewHandler(cfg, path, nil)
	h.SetLocalPassword("test-password")
	s := &Server{cfg: cfg, engine: gin.New(), mgmt: h}
	s.managementRoutesEnabled.Store(true)
	s.registerManagementRoutes()
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/v8/management/credentials", "", 200},
		{"GET", "/v8/management/config", "", 200},
		{"GET", "/v8/management/config.yaml", "", 200},
		{"GET", "/v8/management/plugins", "", 404},
		{"GET", "/v8/management/plugins/store", "", 404},
		{"POST", "/v8/management/plugins/store/test/install", "{}", 404},
		{"GET", "/v0/management/plugins", "", 404},
		{"GET", "/v0/management/plugin-store", "", 404},
		{"POST", "/v0/management/plugin-store/test/install", "{}", 404},
		{"PATCH", "/v8/management/config/plugins/enabled", "true", 404},
		{"PUT", "/v8/management/config/plugins", "{\"enabled\":true}", 404},
		{"PATCH", "/v8/management/config", "{\"plugins\":{\"enabled\":true}}", 400},
		{"PUT", "/v8/management/config.yaml", "plugins: {enabled: true}", 400},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set("Authorization", "Bearer test-password")
			res := httptest.NewRecorder()
			s.engine.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.want, res.Body.String())
			}
			if tc.method == http.MethodGet && strings.Contains(tc.path, "/config") && strings.Contains(res.Body.String(), "plugins") {
				t.Fatal("removed configuration exposed")
			}
		})
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(baseline) {
		t.Fatal("rejected writes or read-only requests changed the saved configuration")
	}
}
