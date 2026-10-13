package pluginhost

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestATIDHostCannotLoadExtensionsFromConfiguration(t *testing.T) {
	dir := t.TempDir()
	enabled := true
	cfg := &config.Config{Plugins: config.PluginsConfig{
		Enabled: true, Dir: filepath.Join(dir, "never-created"),
		Configs: map[string]config.PluginInstanceConfig{"external": {Enabled: &enabled}},
	}}
	host := New()
	host.ApplyConfig(context.Background(), cfg)
	if len(host.RegisteredPlugins()) != 0 {
		t.Fatal("runtime registered a plugin")
	}
	if _, err := os.Stat(cfg.Plugins.Dir); !os.IsNotExist(err) {
		t.Fatal("runtime touched the extension directory")
	}
	if SupportPluginHeaderValue() != "0" {
		t.Fatal("runtime advertised extension support")
	}
	if _, err := defaultPluginLoader().Open(pluginFile{Path: "external.dylib"}, host); err == nil {
		t.Fatal("dynamic loader is available")
	}
}
