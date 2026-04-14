#!/usr/bin/env python3
"""send_event.py — CLI wrapper around :mod:`apogee_hook`.

This is the script that ``.claude/settings.json`` points at. It reads a Claude
Code hook payload from stdin, forwards it to the apogee collector via
:func:`apogee_hook.send_event`, and echoes the original stdin back to stdout so
that Claude Code's hook pipeline is unaffected by our side-channel.

Exit code is always 0. A failing hook would break Claude Code, and there is no
user-facing benefit to surfacing apogee transport errors as non-zero.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

# Allow running this script directly (``python3 send_event.py``) as well as
# being imported. When run as a script the parent directory containing
# ``apogee_hook.py`` has to be on ``sys.path`` explicitly — Python adds the
# script directory automatically only when the module is not inside a package.
_SCRIPT_DIR = Path(__file__).resolve().parent
if str(_SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(_SCRIPT_DIR))

import apogee_hook  # noqa: E402  (sys.path mutation above is intentional)
import apogee_intervention  # noqa: E402


def _parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="send_event.py",
        description=(
            "Forward a Claude Code hook payload to the apogee collector. "
            "Reads JSON from stdin, POSTs it to the collector, and echoes "
            "stdin back to stdout unchanged."
        ),
    )
    parser.add_argument(
        "--source-app",
        required=True,
        help="Source application label stored on every event (e.g. 'my-project').",
    )
    parser.add_argument(
        "--event-type",
        required=True,
        help=(
            "Hook event name. One of: SessionStart, SessionEnd, UserPromptSubmit, "
            "PreToolUse, PostToolUse, PostToolUseFailure, PermissionRequest, "
            "Notification, SubagentStart, SubagentStop, Stop, PreCompact."
        ),
    )
    parser.add_argument(
        "--server-url",
        default=os.environ.get("APOGEE_SERVER_URL", apogee_hook.DEFAULT_SERVER_URL),
        help=(
            "Collector endpoint. Defaults to $APOGEE_SERVER_URL or "
            f"{apogee_hook.DEFAULT_SERVER_URL}."
        ),
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=apogee_hook.DEFAULT_TIMEOUT_SECONDS,
        help="HTTP timeout in seconds (default: 2.0).",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(sys.argv[1:] if argv is None else argv)

    raw_stdin = sys.stdin.read()

    input_data: dict = {}
    if raw_stdin.strip():
        try:
            parsed = json.loads(raw_stdin)
            if isinstance(parsed, dict):
                input_data = parsed
            else:
                print(
                    f"send_event: expected JSON object, got {type(parsed).__name__}",
                    file=sys.stderr,
                )
        except json.JSONDecodeError as exc:
            print(f"send_event: invalid JSON on stdin: {exc}", file=sys.stderr)

    # Operator-intervention hook: for PreToolUse / UserPromptSubmit, ask the
    # collector whether there is a queued operator intervention for this
    # session and, if so, emit the Claude Code decision JSON instead of the
    # default pass-through echo. Any failure inside this call degrades to
    # the plain echo path so Claude Code never breaks.
    decision = None
    try:
        decision = apogee_intervention.handle_hook(
            server_base=args.server_url,
            hook_event=args.event_type,
            input_data=input_data,
            timeout=args.timeout,
        )
    except Exception as exc:  # pragma: no cover - defensive
        print(
            f"send_event: apogee_intervention error: {exc}",
            file=sys.stderr,
        )
        decision = None

    if decision is not None:
        # A claimed intervention replaces the default stdout echo with the
        # Claude Code decision JSON.
        try:
            sys.stdout.write(json.dumps(decision))
            sys.stdout.write("\n")
            sys.stdout.flush()
        except Exception as exc:  # pragma: no cover - defensive
            print(f"send_event: failed to write decision: {exc}", file=sys.stderr)
    elif raw_stdin:
        # Default path: echo stdin back to stdout verbatim so the rest of
        # the Claude Code hook pipeline sees the same payload we received.
        sys.stdout.write(raw_stdin)
        sys.stdout.flush()

    apogee_hook.send_event(
        source_app=args.source_app,
        hook_event_type=args.event_type,
        input_data=input_data,
        server_url=args.server_url,
        timeout=args.timeout,
    )

    return 0


if __name__ == "__main__":
    sys.exit(main())
