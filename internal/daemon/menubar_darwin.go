//go:build darwin

package daemon

// MenubarConfig returns a Config pre-populated with the defaults for
// the `apogee menubar` launchd unit. The caller is expected to
// override BinaryPath (usually via os.Executable) and optionally
// WorkingDir / LogDir / Environment before passing it to
// Manager.Install.
//
// The menubar unit differs from the collector daemon unit in three
// important ways:
//
//   - LSUIElement=true so the process runs as a menu-bar-only Cocoa
//     app (no Dock icon, no main window). The launchd template emits
//     this key only when cfg.LSUIElement is set.
//   - LimitLoadToSessionType="Aqua" so launchd only loads the unit
//     inside a real GUI login session. SSH / background sessions
//     never see the plist, which prevents the menubar process from
//     spinning up headless and burning CPU.
//   - KeepAlive=false because the menubar is interactive — if the
//     user picks "Quit menubar" from the dropdown we do not want
//     launchd to resurrect it until the next login.
//
// RunAtLoad stays true so the process starts the moment the plist is
// bootstrapped (and on every subsequent login, which is the whole
// point of the login-item story).
func MenubarConfig() Config {
	return Config{
		Label:                  MenubarLabel,
		Args:                   []string{"menubar"},
		LogFileBase:            "menubar",
		LSUIElement:            true,
		LimitLoadToSessionType: "Aqua",
		RunAtLoad:              true,
		KeepAlive:              false,
	}
}
