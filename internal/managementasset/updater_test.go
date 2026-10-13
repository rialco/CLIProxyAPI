package managementasset

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type rejectAssetNetwork struct{ t *testing.T }

func (r rejectAssetNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Fatal("the ATID panel must never be downloaded")
	return nil, nil
}

func TestLocalManagementAssetNeverDownloadsOrOverwrites(t *testing.T) {
	old := http.DefaultTransport
	http.DefaultTransport = rejectAssetNetwork{t}
	t.Cleanup(func() { http.DefaultTransport = old })
	dir := t.TempDir()
	if EnsureLatestManagementHTML(context.Background(), dir, "", "https://example.com/releases") {
		t.Fatal("missing local asset reported as present")
	}
	asset := filepath.Join(dir, ManagementFileName)
	original := []byte("<title>ATID CLI Proxy</title>")
	if err := os.WriteFile(asset, original, 0600); err != nil {
		t.Fatal(err)
	}
	StartAutoUpdater(context.Background(), filepath.Join(dir, "config.yaml"))
	if !EnsureLatestManagementHTML(context.Background(), dir, "", "https://example.com/releases") {
		t.Fatal("local panel missing")
	}
	got, err := os.ReadFile(asset)
	if err != nil || string(got) != string(original) {
		t.Fatalf("local panel changed: %v", err)
	}
}

func TestManagementAssetPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANAGEMENT_STATIC_PATH", filepath.Join(dir, ManagementFileName))
	if got := FilePath("unused"); got != filepath.Join(dir, ManagementFileName) {
		t.Fatalf("path=%q", got)
	}
	if got := StaticDir("unused"); got != dir {
		t.Fatalf("directory=%q", got)
	}
}
