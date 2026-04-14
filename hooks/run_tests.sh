#!/usr/bin/env bash
# Run the apogee Python hook unit tests.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR/.."
python3 -m unittest discover -s hooks/tests -t .
