"""apogee_hook — dependency-free helper for posting Claude Code hook events.

This module is the Python side of the apogee wire contract. It mirrors the
``HookEvent`` schema accepted by the apogee collector's ``POST /v1/events``
endpoint (see ``internal/ingest/payload.go``) and is wire-compatible with the
reference hooks from disler's multi-agent-observability project.

Design goals:

* Standard library only. No ``requests``, no ``pydantic``, no ``uv``. The file
  must work on any system with ``python3`` available.
* Never break Claude Code. A network failure, a JSON decode error, or a
  misconfigured collector URL must not raise — log to stderr and continue.
* Flatten the top-level fields the apogee collector reads out of the payload
  so queries do not have to re-parse the nested ``payload`` blob.

The public surface is intentionally small:

* :func:`send_event` — build a :class:`HookEvent`-shaped dict and POST it.
* :func:`read_hook_input` — read and parse the Claude Code stdin contract.
* :func:`extract_top_level_fields` — mirror disler's field-forwarding logic.

All three functions are used by ``send_event.py`` and by the unit tests under
``hooks/tests/``.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

DEFAULT_SERVER_URL = "http://localhost:4100/v1/events"
DEFAULT_TIMEOUT_SECONDS = 2.0
DEFAULT_SOURCE_APP = "unknown"

# Keys that the collector flattens onto the top level of HookEvent. Keep this
# list aligned with internal/ingest/payload.go::HookEvent. Adding a new flat
# field here is a two-step change: add it to the Go struct first, then here.
_FLAT_FIELDS: tuple[str, ...] = (
    "tool_name",
    "tool_use_id",
    "error",
    "is_interrupt",
    "permission_suggestions",
    "agent_id",
    "agent_type",
    "agent_transcript_path",
    "stop_hook_active",
    "notification_type",
    "custom_instructions",
    "source",
    "reason",
    "model_name",
    "prompt",
)


def derive_source_app() -> str:
    """Derive a ``source_app`` label at hook invocation time.

    The intended workflow is: install the hooks once at user scope
    (``~/.claude/settings.json``) and let every Claude Code session
    auto-label itself based on where it was started. Resolution order:

    1. ``APOGEE_SOURCE_APP`` environment variable — explicit override.
    2. ``basename`` of ``git rev-parse --show-toplevel`` when inside a
       git repository. This matches the operator's mental model: one
       repo = one source_app.
    3. ``basename`` of the current working directory as a fallback for
       non-git projects.
    4. :data:`DEFAULT_SOURCE_APP` (``"unknown"``) if every probe fails.

    This function never raises. Failures drop through to the next
    probe and ultimately to the ``"unknown"`` default.
    """
    env = os.environ.get("APOGEE_SOURCE_APP", "").strip()
    if env:
        return env
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True,
            text=True,
            timeout=1.0,
            check=False,
        )
        if result.returncode == 0:
            top = result.stdout.strip()
            if top:
                name = Path(top).name
                if name:
                    return name
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        pass
    try:
        cwd_name = Path.cwd().name
        if cwd_name:
            return cwd_name
    except OSError:
        pass
    return DEFAULT_SOURCE_APP


def read_hook_input() -> dict[str, Any]:
    """Read a JSON hook payload from ``sys.stdin``.

    Claude Code passes hook input as a single JSON object on stdin. If the
    stream is empty or invalid we return ``{}`` and log to stderr — we never
    raise, because doing so would break the user's Claude Code session.
    """
    try:
        raw = sys.stdin.read()
    except Exception as exc:  # pragma: no cover - extremely defensive
        print(f"apogee_hook: failed to read stdin: {exc}", file=sys.stderr)
        return {}
    if not raw.strip():
        return {}
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        print(f"apogee_hook: failed to parse stdin JSON: {exc}", file=sys.stderr)
        return {}
    if not isinstance(data, dict):
        print(
            f"apogee_hook: expected JSON object on stdin, got {type(data).__name__}",
            file=sys.stderr,
        )
        return {}
    return data


def extract_top_level_fields(event_type: str, input_data: dict[str, Any]) -> dict[str, Any]:
    """Return the subset of ``input_data`` that is promoted to HookEvent's
    top level.

    The collector reads these fields directly instead of re-parsing the nested
    payload. Missing fields are simply not returned (the JSON encoder will
    omit them on the Go side thanks to ``omitempty``).

    ``event_type`` is accepted for forward compatibility — today we forward
    every known flat field unconditionally if it is present, matching the
    reference behaviour in disler's ``send_event.py``.
    """
    del event_type  # reserved for future per-event filtering
    if not isinstance(input_data, dict):
        return {}
    flattened: dict[str, Any] = {}
    for key in _FLAT_FIELDS:
        if key in input_data:
            flattened[key] = input_data[key]
    return flattened


def _now_millis() -> int:
    return int(time.time() * 1000)


def build_event(
    *,
    source_app: str,
    hook_event_type: str,
    input_data: dict[str, Any],
    summary: str | None = None,
    chat: list[Any] | None = None,
) -> dict[str, Any]:
    """Build a HookEvent-shaped dict without sending it.

    Exposed for tests and for callers that want to inspect/modify the payload
    before POSTing. ``send_event`` calls this internally.
    """
    session_id = ""
    if isinstance(input_data, dict):
        sid = input_data.get("session_id")
        if isinstance(sid, str) and sid:
            session_id = sid

    event: dict[str, Any] = {
        "source_app": source_app,
        "session_id": session_id or "unknown",
        "hook_event_type": hook_event_type,
        "timestamp": _now_millis(),
        "payload": input_data if isinstance(input_data, dict) else {},
    }

    flattened = extract_top_level_fields(hook_event_type, input_data)
    event.update(flattened)

    if summary:
        event["summary"] = summary
    if chat is not None:
        event["chat"] = chat

    return event


def send_event(
    source_app: str,
    hook_event_type: str,
    input_data: dict[str, Any],
    server_url: str = DEFAULT_SERVER_URL,
    timeout: float = DEFAULT_TIMEOUT_SECONDS,
    summary: str | None = None,
    chat: list[Any] | None = None,
) -> None:
    """POST a HookEvent to the apogee collector.

    This function never raises. All failures are logged to stderr so that
    Claude Code's hook execution is not disrupted. Return value is ``None``
    — callers should treat this as fire-and-forget.
    """
    event = build_event(
        source_app=source_app,
        hook_event_type=hook_event_type,
        input_data=input_data,
        summary=summary,
        chat=chat,
    )

    try:
        body = json.dumps(event).encode("utf-8")
    except (TypeError, ValueError) as exc:
        print(f"apogee_hook: failed to encode event JSON: {exc}", file=sys.stderr)
        return

    req = urllib.request.Request(
        server_url,
        data=body,
        headers={
            "Content-Type": "application/json",
            "User-Agent": "apogee-hook/0.0.0-dev",
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status = getattr(resp, "status", None)
            if status is not None and status >= 400:
                print(
                    f"apogee_hook: collector returned HTTP {status}",
                    file=sys.stderr,
                )
    except urllib.error.URLError as exc:
        print(f"apogee_hook: network error posting event: {exc}", file=sys.stderr)
    except Exception as exc:  # pragma: no cover - defensive
        print(f"apogee_hook: unexpected error posting event: {exc}", file=sys.stderr)


__all__ = [
    "DEFAULT_SERVER_URL",
    "DEFAULT_SOURCE_APP",
    "DEFAULT_TIMEOUT_SECONDS",
    "build_event",
    "derive_source_app",
    "extract_top_level_fields",
    "read_hook_input",
    "send_event",
]
