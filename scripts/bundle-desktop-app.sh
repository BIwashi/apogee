#!/usr/bin/env bash
# bundle-desktop-app.sh — wrap a plain Go binary into a minimal macOS
# application bundle without any wails CLI / Xcode app project.
#
# Usage:
#   scripts/bundle-desktop-app.sh <binary-path> <version>
#
# The script writes an Apogee.app tree next to the input binary and then
# REPLACES the input binary file with a tiny launcher shim (~400 bytes)
# that execs the real Mach-O inside the .app. This is what keeps the
# release zip small: without the shim, goreleaser's archive step includes
# both the raw universal binary at the zip root AND the copy inside
# Apogee.app/Contents/MacOS/, doubling the download size to ~88 MB. With
# the shim, the zip is just the .app tree plus a tiny forwarding script,
# for a ~44 MB total — 1x the binary instead of 2x.
#
# The shim is idempotent — calling the script a second time replaces both
# the .app and the shim with fresh copies.
#
# We intentionally do NOT code-sign or notarize here. apogee is distributed
# unsigned today; brew cask strips the com.apple.quarantine xattr on
# install so Gatekeeper does not block users. If we ever pick up an Apple
# Developer ID, wire `codesign --deep --sign "Developer ID Application: ..."`
# + `xcrun notarytool submit` into this same script.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <binary-path> <version>" >&2
  exit 2
fi

BIN_PATH="$1"
VERSION="$2"

if [ ! -f "$BIN_PATH" ]; then
  echo "bundle: binary not found at $BIN_PATH" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ICON_SRC="$REPO_ROOT/assets/branding/apogee-icon-256.png"

BIN_DIR="$(cd "$(dirname "$BIN_PATH")" && pwd)"
APP_DIR="$BIN_DIR/Apogee.app"
CONTENTS="$APP_DIR/Contents"
MACOS_DIR="$CONTENTS/MacOS"
RES_DIR="$CONTENTS/Resources"

# Clean any previous bundle so we never end up with stale files from an
# earlier run (e.g. a version bump without a goreleaser clean).
rm -rf "$APP_DIR"
mkdir -p "$MACOS_DIR" "$RES_DIR"

# The executable inside Contents/MacOS must match CFBundleExecutable in
# Info.plist. Copy rather than move so we can then overwrite $BIN_PATH
# with the launcher shim below — goreleaser's archive step expects a
# file at $BIN_PATH and treats it as the build output.
cp "$BIN_PATH" "$MACOS_DIR/apogee-desktop"
chmod +x "$MACOS_DIR/apogee-desktop"

# Build an .icns from the 256px branding PNG. sips + iconutil ship with
# every macOS install, so we do not pull in extra tooling. If the source
# PNG is missing (e.g. a stripped release checkout) we skip the icon
# rather than failing — the .app still launches without one.
if [ -f "$ICON_SRC" ]; then
  ICONSET="$(mktemp -d)/apogee.iconset"
  mkdir -p "$ICONSET"
  # Apple wants 10 sizes in the iconset. Downscaling from 256 to anything
  # larger is a no-op (sips would upscale), so we only populate the sizes
  # <= 256 and let macOS interpolate the rest. The Dock still picks a
  # reasonable icon from what we provide.
  for size in 16 32 64 128 256; do
    sips -z "$size" "$size" "$ICON_SRC" \
      --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  done
  cp "$ICONSET/icon_16x16.png"   "$ICONSET/icon_16x16@2x.png"   2>/dev/null || true
  cp "$ICONSET/icon_32x32.png"   "$ICONSET/icon_32x32@2x.png"   2>/dev/null || true
  cp "$ICONSET/icon_128x128.png" "$ICONSET/icon_128x128@2x.png" 2>/dev/null || true
  iconutil -c icns "$ICONSET" -o "$RES_DIR/apogee.icns"
  rm -rf "$(dirname "$ICONSET")"
  ICON_KEY="<key>CFBundleIconFile</key><string>apogee</string>"
else
  echo "bundle: icon source $ICON_SRC missing, skipping iconset" >&2
  ICON_KEY=""
fi

# Minimal Info.plist. LSMinimumSystemVersion 11.0 matches the floor we
# inherit from the DuckDB cgo toolchain — older releases of macOS are
# already unsupported upstream. NSHighResolutionCapable keeps the
# WKWebView sharp on Retina displays.
cat >"$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>Apogee</string>
    <key>CFBundleDisplayName</key>
    <string>Apogee</string>
    <key>CFBundleIdentifier</key>
    <string>dev.apogee.desktop</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleExecutable</key>
    <string>apogee-desktop</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleSupportedPlatforms</key>
    <array>
        <string>MacOSX</string>
    </array>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    ${ICON_KEY}
</dict>
</plist>
PLIST

# Replace the raw universal Mach-O that was at $BIN_PATH with a small
# POSIX shell launcher. The launcher forwards to the real binary inside
# the .app, which is the one file that actually needs the universal
# Mach-O. This keeps the release zip small while still giving users a
# CLI entry point — brew's cask `binary` directive symlinks
# /opt/homebrew/bin/apogee-desktop to this shim, so `apogee-desktop`
# from a terminal still opens the Wails window.
#
# Two search paths:
#   1. /Applications/Apogee.app — the brew cask's `app "Apogee.app"`
#      directive installs the bundle here, and this is the primary path
#      used when the cask is installed.
#   2. Sibling Apogee.app relative to $0 — used when a user downloads
#      the release zip manually, extracts it somewhere like
#      ~/Downloads/apogee-desktop/, and runs ./apogee-desktop directly
#      without going through brew.
cat >"$BIN_PATH" <<'SHIM'
#!/bin/sh
# apogee-desktop launcher shim. Forwards to the real Wails binary
# bundled inside Apogee.app. Written by scripts/bundle-desktop-app.sh
# as part of the goreleaser build — see that script for context.
set -e

PRIMARY="/Applications/Apogee.app/Contents/MacOS/apogee-desktop"
if [ -x "$PRIMARY" ]; then
    exec "$PRIMARY" "$@"
fi

# Resolve the directory of this script, following symlinks, so we can
# find a sibling Apogee.app when the release zip was extracted manually.
SELF="$0"
while [ -L "$SELF" ]; do
    LINK=$(readlink "$SELF")
    case "$LINK" in
        /*) SELF="$LINK" ;;
        *)  SELF="$(dirname "$SELF")/$LINK" ;;
    esac
done
SIBLING="$(cd "$(dirname "$SELF")" && pwd)/Apogee.app/Contents/MacOS/apogee-desktop"
if [ -x "$SIBLING" ]; then
    exec "$SIBLING" "$@"
fi

echo "apogee-desktop: could not find Apogee.app." >&2
echo "  Tried: $PRIMARY" >&2
echo "  Tried: $SIBLING" >&2
echo "  Install via: brew install --cask BIwashi/tap/apogee-desktop" >&2
exit 1
SHIM
chmod +x "$BIN_PATH"

echo "bundle: wrote $APP_DIR (version ${VERSION}) + launcher shim at $BIN_PATH"
