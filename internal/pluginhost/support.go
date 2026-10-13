package pluginhost

// SupportPluginHeaderValue preserves the wire contract for clients of the core API.
// ATID CLI Proxy never loads dynamic extensions, regardless of CGO availability.
func SupportPluginHeaderValue() string {
	return "0"
}
