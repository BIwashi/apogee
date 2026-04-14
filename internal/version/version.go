// Package version exposes the apogee build version string.
//
// The value is embedded at compile time. Future releases will override it via
// -ldflags "-X github.com/BIwashi/apogee/internal/version.Version=..." so that
// nightly builds and tagged releases advertise the correct identifier.
package version

// Version is the current apogee release identifier.
const Version = "0.0.0-dev"
