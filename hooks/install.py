#!/usr/bin/env python3
"""install.py — standalone Python installer for the apogee hooks.

This script is a fallback for users who want to install the hooks without the
Go ``apogee`` binary. It performs the same edits to ``.claude/settings.json``
that ``apogee init`` performs, using the hook scripts sitting next to it.

Usage::

    python3 hooks/install.py --target ./.claude --source-app my-project

The canonical path is ``apogee init``. This script exists as a bootstrap so
that the Python hooks can be installed from the repo tarball even if the Go
binary is not built yet.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import tempfile
from pathlib import Path

HOOK_EVENTS: tuple[str, ...] = (
    "SessionStart",
    "SessionEnd",
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "PermissionRequest",
    "Notification",
    "SubagentStart",
    "SubagentStop",
    "Stop",
    "PreCompact",
)

DEFAULT_SERVER_URL = "http://localhost:4100/v1/events"


def _build_command(send_event_path: Path, source_app: str, event_type: str, server_url: str) -> str:
    return (
        f"python3 {send_event_path} "
        f"--source-app {source_app} "
        f"--event-type {event_type} "
        f"--server-url {server_url}"
    )


def _atomic_write(path: Path, data: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=".apogee-", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(data)
        os.replace(tmp, path)
    except Exception:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass
        raise


def _merge(settings: dict, send_event_path: Path, source_app: str, server_url: str, force: bool) -> tuple[int, int]:
    hooks_section = settings.setdefault("hooks", {})
    if not isinstance(hooks_section, dict):
        print("install: existing 'hooks' is not an object; refusing to rewrite", file=sys.stderr)
        sys.exit(2)

    added = 0
    skipped = 0
    prefix = f"python3 {send_event_path}"

    for event in HOOK_EVENTS:
        entries = hooks_section.setdefault(event, [])
        if not isinstance(entries, list):
            print(f"install: hooks.{event} is not a list; skipping", file=sys.stderr)
            skipped += 1
            continue

        already = False
        for entry in entries:
            if not isinstance(entry, dict):
                continue
            inner = entry.get("hooks", [])
            if not isinstance(inner, list):
                continue
            for h in inner:
                if isinstance(h, dict) and isinstance(h.get("command"), str) and h["command"].startswith(prefix):
                    already = True
                    break
            if already:
                break

        if already and not force:
            skipped += 1
            continue

        # Strip any prior apogee entries when forcing.
        if force:
            filtered: list = []
            for entry in entries:
                if not isinstance(entry, dict):
                    filtered.append(entry)
                    continue
                inner = entry.get("hooks", [])
                if not isinstance(inner, list):
                    filtered.append(entry)
                    continue
                kept = [h for h in inner if not (isinstance(h, dict) and isinstance(h.get("command"), str) and h["command"].startswith(prefix))]
                if kept:
                    filtered.append({**entry, "hooks": kept})
            entries = filtered
            hooks_section[event] = entries

        command = _build_command(send_event_path, source_app, event, server_url)
        entries.append({"hooks": [{"type": "command", "command": command}]})
        added += 1

    return added, skipped


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="apogee-install", description="Install apogee hooks into .claude/settings.json")
    parser.add_argument("--target", default="./.claude", help="Claude Code settings directory (default: ./.claude)")
    parser.add_argument("--source-app", default=None, help="Source app label (default: directory name)")
    parser.add_argument("--server-url", default=DEFAULT_SERVER_URL, help="Collector URL")
    parser.add_argument("--force", action="store_true", help="Overwrite existing apogee hook entries")
    parser.add_argument("--dry-run", action="store_true", help="Print plan without writing")
    args = parser.parse_args(argv)

    target_dir = Path(os.path.expanduser(args.target)).resolve()
    settings_path = target_dir / "settings.json"
    send_event_path = Path(__file__).resolve().parent / "send_event.py"
    source_app = args.source_app or target_dir.parent.name or "apogee"

    if not send_event_path.exists():
        print(f"install: send_event.py not found at {send_event_path}", file=sys.stderr)
        return 2

    if settings_path.exists():
        try:
            settings = json.loads(settings_path.read_text(encoding="utf-8"))
            if not isinstance(settings, dict):
                print(f"install: {settings_path} is not a JSON object", file=sys.stderr)
                return 2
        except json.JSONDecodeError as exc:
            print(f"install: failed to parse {settings_path}: {exc}", file=sys.stderr)
            return 2
    else:
        settings = {}

    added, skipped = _merge(settings, send_event_path, source_app, args.server_url, args.force)

    serialised = json.dumps(settings, indent=2, sort_keys=True) + "\n"

    if args.dry_run:
        print(f"install: would write {settings_path}")
        print(f"install: added {added}, skipped {skipped}")
        print(serialised)
        return 0

    _atomic_write(settings_path, serialised)
    print(f"install: wrote {settings_path} (added {added}, skipped {skipped})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
