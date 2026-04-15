#!/usr/bin/env bash
# generate-branding.sh — regenerate every raster under assets/branding/ and
# the Next.js app icons from a single typographic source (Artemis Inter).
#
# Run from the repo root:
#     bash scripts/generate-branding.sh
#
# Design source: NASA's "Artemis Graphic Standards Guide" (Dec 16 2021).
# The Artemis brand uses Artemis Inter as the primary display face and a
# fixed six-color palette keyed off NASA Red, NASA Blue, and Earth Blue.
# This script reuses those exact values so apogee's branding reads as a
# NASA-Artemis-adjacent observability product without bundling any NASA
# trademark.
#
# Requires ImageMagick 7 (`magick`).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FONT_DISPLAY="${REPO_ROOT}/web/public/fonts/Artemis_Inter.otf"
FONT_SUBTITLE="${REPO_ROOT}/web/public/fonts/SpaceGrotesk-Medium.ttf"
BRANDING_DIR="${REPO_ROOT}/assets/branding"
WEB_APP_DIR="${REPO_ROOT}/web/app"
WEB_PUBLIC_DIR="${REPO_ROOT}/web/public"

if ! command -v magick >/dev/null 2>&1; then
  echo "error: ImageMagick (magick) is required" >&2
  exit 1
fi

for f in "$FONT_DISPLAY" "$FONT_SUBTITLE"; do
  if [[ ! -f "$f" ]]; then
    echo "error: missing font $f" >&2
    exit 1
  fi
done

mkdir -p "$BRANDING_DIR"

# ── Artemis Graphic Standards palette (Dec 16 2021, page 6) ───
# NASA Red       #FC3D21    Pantone 185 CVC — the "path to Mars" trajectory
# NASA Blue      #0B3D91    Pantone 286 C   — NASA Insignia field blue
# Earth Blue     #27AAE0    Pantone 298 C   — Artemis crescent horizon
# Shadow Gray    #58595B    Pantone 425 C   — Artemis wordmark on white
# Space Gray     #A7A9AC    Pantone Cool 6C — tertiary text
# White          #FFFFFF
NASA_RED="#FC3D21"
NASA_BLUE="#0B3D91"
EARTH_BLUE="#27AAE0"
SHADOW_GRAY="#58595B"
SPACE_GRAY="#A7A9AC"
WHITE="#FFFFFF"
DEEP_BG="#06080f"  # apogee-specific deep-space surface, matches --bg-deepspace

WORDMARK="APOGEE"
SUBTITLE="OBSERVABILITY FOR CLAUDE CODE AGENTS"

# ── Helpers ────────────────────────────────────────────────────
# Cool Horizon Visual (Artemis guide pages 1, 7, and the cover) —
# a single seamless radial gradient where the brightest point sits
# just below the canvas's bottom edge, fading smoothly through Earth
# Blue into NASA Blue at the upper corners. The guide examples are
# always one continuous gradient; never a composite of a "field" and
# a "glow" with a hard boundary.
#
# Implementation trick: build the gradient on a canvas that is twice
# as tall, then crop to the top half. The radial gradient's center
# falls at (w/2, h) which is exactly the bottom edge of the cropped
# region, so the brightest point is the horizon line and the visible
# portion is a smooth half-circle of falloff. This produces zero
# compositing seams because there is only one source layer.
render_cool_horizon () {
  local out="$1" w="$2" h="$3"
  local doubleH=$((h * 2))
  magick -size "${w}x${doubleH}" \
    radial-gradient:"${EARTH_BLUE}-${NASA_BLUE}" \
    -crop "${w}x${h}+0+0" +repage \
    "$out"
}

# Render the Artemis "trajectory" — a short NASA Red crescent that gives
# the icon a touch of the official Artemis mark without reproducing it.
render_trajectory () {
  local out="$1" w="$2" h="$3"
  magick -size "${w}x${h}" "xc:none" \
    -fill "${NASA_RED}" -stroke none \
    -draw "translate $((w / 2)),$((h / 2)) rotate -25 \
           ellipse 0,0 $((w / 3)),$((h / 6)) -30,210" \
    "$out"
}

# ── Banner (972×352) ───────────────────────────────────────────
# Primary README hero. Cool Horizon Visual background, Artemis Inter
# wordmark in white with wide NASA-style tracking, subtitle in Space
# Grotesk medium. The wordmark sits slightly above the visual centre
# so the brightest part of the horizon glow forms a luminous base for
# the type rather than competing with it — mirroring the page-1 cover
# composition where the Artemis logo floats above its glow.
render_banner () {
  local out="$1"
  local w=972 h=352
  local bg="${BRANDING_DIR}/.banner-bg.png"
  local wordmark="${BRANDING_DIR}/.banner-wordmark.png"
  local subtitle="${BRANDING_DIR}/.banner-subtitle.png"

  render_cool_horizon "$bg" $w $h

  magick -background none -fill "${WHITE}" \
    -font "$FONT_DISPLAY" -pointsize 132 -kerning 32 \
    "label:${WORDMARK}" "$wordmark"

  magick -background none -fill "${SPACE_GRAY}" \
    -font "$FONT_SUBTITLE" -pointsize 18 -kerning 4 \
    "label:${SUBTITLE}" "$subtitle"

  magick "$bg" \
    "$wordmark" -gravity center -geometry +0-44 -compose over -composite \
    "$subtitle" -gravity center -geometry +0+58 -compose over -composite \
    "$out"

  rm -f "$bg" "$wordmark" "$subtitle"
}

# ── Logo dark (1080×406) ───────────────────────────────────────
# Wordmark-only variant on the deep-space field. No subtitle, no
# horizon glow — used in contexts that need the full-width wordmark
# without the hero treatment.
render_logo_dark () {
  local out="$1"
  local w=1080 h=406
  local canvas="${BRANDING_DIR}/.logo-canvas.png"
  local wordmark="${BRANDING_DIR}/.logo-wordmark.png"

  magick -size "${w}x${h}" "xc:${DEEP_BG}" "$canvas"

  magick -background none -fill "${WHITE}" \
    -font "$FONT_DISPLAY" -pointsize 160 -kerning 40 \
    "label:${WORDMARK}" "$wordmark"

  magick "$canvas" \
    "$wordmark" -gravity center -geometry +0+0 -compose over -composite \
    "$out"

  rm -f "$canvas" "$wordmark"
}

# ── Logo light (1080×406) ──────────────────────────────────────
# White field, Shadow Gray wordmark. Mirrors the Artemis guide's
# page-4 "Color version positive" rule: the wordmark reads as Shadow
# Gray on white so it does not fight with the NASA Red trajectory if
# one is drawn alongside it.
render_logo_light () {
  local out="$1"
  local w=1080 h=406
  local canvas="${BRANDING_DIR}/.logo-canvas.png"
  local wordmark="${BRANDING_DIR}/.logo-wordmark.png"

  magick -size "${w}x${h}" "xc:${WHITE}" "$canvas"

  magick -background none -fill "${SHADOW_GRAY}" \
    -font "$FONT_DISPLAY" -pointsize 160 -kerning 40 \
    "label:${WORDMARK}" "$wordmark"

  magick "$canvas" \
    "$wordmark" -gravity center -geometry +0+0 -compose over -composite \
    "$out"

  rm -f "$canvas" "$wordmark"
}

# ── Icon (256×256) ─────────────────────────────────────────────
# Square app icon: deep-space field, a big "A" glyph in Artemis Inter,
# with a single NASA Red trajectory stroke behind it — a tiny nod to
# the Artemis mark's red arc.
render_icon () {
  local out="$1"
  local w=256 h=256
  local canvas="${BRANDING_DIR}/.icon-canvas.png"
  local glyph="${BRANDING_DIR}/.icon-glyph.png"
  local trajectory="${BRANDING_DIR}/.icon-trajectory.png"

  magick -size "${w}x${h}" "xc:${DEEP_BG}" "$canvas"

  render_trajectory "$trajectory" $w $h

  magick -background none -fill "${WHITE}" \
    -font "$FONT_DISPLAY" -pointsize 230 -kerning 0 \
    "label:A" "$glyph"

  magick "$canvas" \
    "$trajectory" -gravity center -geometry +0+10 -compose over -composite \
    "$glyph" -gravity center -geometry +0-6 -compose over -composite \
    "$out"

  rm -f "$canvas" "$glyph" "$trajectory"
}

echo ">> regenerating assets/branding/apogee-banner.png"
render_banner "${BRANDING_DIR}/apogee-banner.png"

echo ">> regenerating assets/branding/apogee-logo-dark.png"
render_logo_dark "${BRANDING_DIR}/apogee-logo-dark.png"

echo ">> regenerating assets/branding/apogee-logo-light.png"
render_logo_light "${BRANDING_DIR}/apogee-logo-light.png"

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
