//go:build !darwin

package daemon

// MenubarConfig returns a Config with only the label populated on
// non-darwin platforms. The menubar is macOS-only, so the CLI layer
// short-circuits the install/uninstall/status flow with an
// ErrNotSupported before a Manager is ever asked to consume this
// config. The helper still exists so cross-platform callers can
// reference MenubarLabel via a single code path.
func MenubarConfig() Config {
	return Config{Label: MenubarLabel, Args: []string{"menubar"}, LogFileBase: "menubar"}
}
