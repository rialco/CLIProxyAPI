package pluginhost

import "fmt"

type unsupportedLoader struct{}

func (unsupportedLoader) Open(file pluginFile, host *Host) (pluginClient, error) {
	return nil, fmt.Errorf("ATID CLI Proxy does not support dynamic plugins: %s", file.Path)
}

func defaultPluginLoader() pluginLoader {
	return unsupportedLoader{}
}
