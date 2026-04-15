#!/usr/bin/env bash
# generate-branding.sh — regenerate every raster under assets/branding/ and
# the Next.js app icons from a single typographic source (Space Grotesk).
#
# Run from the repo root:
#     bash scripts/generate-branding.sh
#
# Requires ImageMagick 7 (`magick`). The script uses Space Grotesk Bold
# (SIL Open Font License 1.1) from web/public/fonts/ as the single font
# source so the brand feels consistent across every PNG.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FONT_BOLD="${REPO_ROOT}/web/public/fonts/SpaceGrotesk-Bold.ttf"
FONT_MEDIUM="${REPO_ROOT}/web/public/fonts/SpaceGrotesk-Medium.ttf"
BRANDING_DIR="${REPO_ROOT}/assets/branding"
WEB_APP_DIR="${REPO_ROOT}/web/app"
WEB_PUBLIC_DIR="${REPO_ROOT}/web/public"

if ! command -v magick >/dev/null 2>&1; then
  echo "error: ImageMagick (magick) is required" >&2
  exit 1
fi

for f in "$FONT_BOLD" "$FONT_MEDIUM"; do
  if [[ ! -f "$f" ]]; then
    echo "error: missing font $f" >&2
    exit 1
  fi
done

mkdir -p "$BRANDING_DIR"

DARK_BG="#06080f"
LIGHT_BG="#ffffff"
DARK_TEXT="#ffffff"
LIGHT_TEXT="#0b0d14"
SUBTITLE_TEXT="#8892a6"

# ── Space the wordmark by hand: ImageMagick's -kerning adds a uniform
# extra advance between glyphs which mimics the 0.12em letter-spacing we
# use in CSS. With a pointsize of 128, 0.12em ≈ 15px.
WORDMARK="APOGEE"
SUBTITLE="OBSERVABILITY FOR CLAUDE CODE AGENTS"

# Helper: render a text label to a transparent PNG, then composite it
# onto a background canvas of a given size. The text is horizontally
# and vertically centered.
render_wordmark () {
  local out="$1" w="$2" h="$3" bg="$4" fg="$5" point="$6" kern="$7"
  magick -size "${w}x${h}" "xc:${bg}" \
    -font "$FONT_BOLD" -fill "$fg" -pointsize "$point" -kerning "$kern" \
    -gravity center -annotate +0+0 "$WORDMARK" \
    "$out"
}

render_banner () {
  local out="$1"
  # Banner is 972×352 dark. Wordmark + subtitle stacked.
  local canvas="${BRANDING_DIR}/.banner-canvas.png"
  local wordmark="${BRANDING_DIR}/.banner-wordmark.png"
  local subtitle="${BRANDING_DIR}/.banner-subtitle.png"

  magick -size 972x352 "xc:${DARK_BG}" "$canvas"

  magick -background "${DARK_BG}" -fill "${DARK_TEXT}" \
    -font "$FONT_BOLD" -pointsize 128 -kerning 15 \
    "label:${WORDMARK}" "$wordmark"

  magick -background "${DARK_BG}" -fill "${SUBTITLE_TEXT}" \
    -font "$FONT_MEDIUM" -pointsize 20 -kerning 4 \
    "label:${SUBTITLE}" "$subtitle"

  # Composite wordmark centered slightly above middle, subtitle below.
  magick "$canvas" \
    "$wordmark" -gravity center -geometry +0-26 -composite \
    "$subtitle" -gravity center -geometry +0+72 -composite \
    "$out"

  rm -f "$canvas" "$wordmark" "$subtitle"
}

render_logo () {
  local out="$1" bg="$2" fg="$3"
  local canvas="${BRANDING_DIR}/.logo-canvas.png"
  local wordmark="${BRANDING_DIR}/.logo-wordmark.png"

  magick -size 1080x406 "xc:${bg}" "$canvas"

  magick -background "${bg}" -fill "${fg}" \
    -font "$FONT_BOLD" -pointsize 156 -kerning 18 \
    "label:${WORDMARK}" "$wordmark"

  magick "$canvas" \
    "$wordmark" -gravity center -geometry +0+0 -composite \
    "$out"

  rm -f "$canvas" "$wordmark"
}

render_icon () {
  local out="$1"
  local canvas="${BRANDING_DIR}/.icon-canvas.png"
  local glyph="${BRANDING_DIR}/.icon-glyph.png"

  magick -size 256x256 "xc:${DARK_BG}" "$canvas"

  magick -background "${DARK_BG}" -fill "${DARK_TEXT}" \
    -font "$FONT_BOLD" -pointsize 220 -kerning 0 \
    "label:A" "$glyph"

  magick "$canvas" \
    "$glyph" -gravity center -geometry +0-8 -composite \
    "$out"

  rm -f "$canvas" "$glyph"
}

echo ">> regenerating assets/branding/apogee-banner.png"
render_banner "${BRANDING_DIR}/apogee-banner.png"

echo ">> regenerating assets/branding/apogee-logo-dark.png"
render_logo "${BRANDING_DIR}/apogee-logo-dark.png" "$DARK_BG" "$DARK_TEXT"

echo ">> regenerating assets/branding/apogee-logo-light.png"
render_logo "${BRANDING_DIR}/apogee-logo-light.png" "$LIGHT_BG" "$LIGHT_TEXT"

echo ">> regenerating assets/branding/apogee-icon-256.png"
render_icon "${BRANDING_DIR}/apogee-icon-256.png"

# Derive the Next.js icons from the 256 master so the favicon/apple-touch
# icons stay in sync with the brand mark.
ICON_MASTER="${BRANDING_DIR}/apogee-icon-256.png"

echo ">> regenerating web/app/icon.png"
magick "$ICON_MASTER" -resize 256x256 "${WEB_APP_DIR}/icon.png"

echo ">> regenerating web/app/apple-icon.png"
magick "$ICON_MASTER" -resize 180x180 "${WEB_APP_DIR}/apple-icon.png"

echo ">> regenerating web/public/favicon.png"
magick "$ICON_MASTER" -resize 32x32 "${WEB_PUBLIC_DIR}/favicon.png"

echo "done."
