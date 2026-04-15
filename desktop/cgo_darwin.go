//go:build darwin

package main

// Explicitly link UniformTypeIdentifiers.framework. WebKit in recent
// macOS SDKs has a weak reference to `_OBJC_CLASS_$_UTType`
// (UTType lives in UniformTypeIdentifiers, macOS 11+). Without an
// explicit link directive, a release build that passes `-ldflags
// "-s -w"` strips the linker hints that would otherwise resolve the
// framework automatically, producing:
//
//	Undefined symbols for architecture arm64:
//	  "_OBJC_CLASS_$_UTType", referenced from: ...
//	ld: symbol(s) not found for architecture arm64
//
// Declaring the framework here forces it into the final link step
// regardless of the surrounding ldflags, so the release build stays
// small (-s -w is still applied) while the WebKit reference resolves.
//
// Safe to add unconditionally on darwin — UniformTypeIdentifiers has
// shipped in every supported macOS release since Big Sur and this
// project's LSMinimumSystemVersion is 11.0 anyway.

/*
#cgo darwin LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
