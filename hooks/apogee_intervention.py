"""apogee_intervention — operator-intervention side of the hook contract.

This module is called by ``send_event.py`` before it POSTs the hook event to
``/v1/events``. On ``PreToolUse`` and ``UserPromptSubmit`` events it tries to
claim the next pending operator intervention for the session. If the claim
succeeds, it writes the Claude Code decision JSON to stdout (so the agent
sees the block text or the additional context) and then reports delivery back
to the collector.

All network calls are best-effort. A failure at any stage logs to stderr and
returns without raising so Claude Code's hook pipeline is never broken.
"""

from __future__ import annotations

import json
import sys
import urllib.error
import urllib.request
from typing import Any

# Hook event names that are allowed to carry an intervention decision.
# PostToolUse / SessionStart / etc. have no place to inject text, so the
# claim step is skipped entirely for those events.
CLAIMABLE_HOOKS = frozenset({"PreToolUse", "UserPromptSubmit"})

# Delivery mode constants. Keep in sync with duckdb.Intervention* and the
# TypeScript mirror in web/app/lib/api-types.ts.
MODE_INTERRUPT = "interrupt"
MODE_CONTEXT = "context"
MODE_BOTH = "both"

_DEFAULT_TIMEOUT_SECONDS = 2.0


def claim_intervention(
    *,
    server_base: str,
    session_id: str,
    turn_id: str,
    hook_event: str,
    timeout: float = _DEFAULT_TIMEOUT_SECONDS,
) -> dict[str, Any] | None:
    """Best-effort call to ``POST /v1/sessions/<sid>/interventions/claim``.

    Returns the decoded intervention payload when the collector hands one to
    this hook, ``None`` otherwise. Network errors and unexpected responses
    are logged to stderr and swallowed.
    """
    if hook_event not in CLAIMABLE_HOOKS:
        return None
    if not session_id:
        return None

    url = f"{_strip_events_suffix(server_base)}/v1/sessions/{session_id}/interventions/claim"
    body = json.dumps({"hook_event": hook_event, "turn_id": turn_id or ""}).encode("utf-8")
    req = urllib.request.Request(
        url,
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
            if status == 204 or status is None:
                return None
            if status >= 400:
                print(
                    f"apogee_intervention: claim returned HTTP {status}",
                    file=sys.stderr,
                )
                return None
            raw = resp.read()
            if not raw:
                return None
            try:
                parsed = json.loads(raw)
            except json.JSONDecodeError as exc:
                print(
                    f"apogee_intervention: failed to decode claim response: {exc}",
                    file=sys.stderr,
                )
                return None
            if isinstance(parsed, dict):
                iv = parsed.get("intervention")
                if isinstance(iv, dict):
                    return iv
            return None
    except urllib.error.HTTPError as exc:
        if exc.code == 204:
            return None
        print(
            f"apogee_intervention: claim HTTP error: {exc}",
            file=sys.stderr,
        )
        return None
    except urllib.error.URLError as exc:
        print(
            f"apogee_intervention: claim network error: {exc}",
            file=sys.stderr,
        )
        return None
    except Exception as exc:  # pragma: no cover - defensive
        print(
            f"apogee_intervention: claim unexpected error: {exc}",
            file=sys.stderr,
        )
        return None


def mark_delivered(
    *,
    server_base: str,
    intervention_id: str,
    hook_event: str,
    timeout: float = _DEFAULT_TIMEOUT_SECONDS,
) -> None:
    """Best-effort ``POST /v1/interventions/<id>/delivered``.

    Errors are logged and swallowed. Returns nothing.
    """
    if not intervention_id:
        return
    url = f"{_strip_events_suffix(server_base)}/v1/interventions/{intervention_id}/delivered"
    body = json.dumps({"hook_event": hook_event}).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=body,
        headers={
            "Content-Type": "application/json",
            "User-Agent": "apogee-hook/0.0.0-dev",
        },
        method="POST",
    )
    try:
        urllib.request.urlopen(req, timeout=timeout).close()
    except urllib.error.URLError as exc:
        print(
            f"apogee_intervention: delivered network error: {exc}",
            file=sys.stderr,
        )
    except Exception as exc:  # pragma: no cover - defensive
        print(
            f"apogee_intervention: delivered unexpected error: {exc}",
            file=sys.stderr,
        )


def decision_json(iv: dict[str, Any], hook_event: str) -> dict[str, Any] | None:
    """Return the Claude Code hook decision object for an intervention.

    Returns ``None`` when the intervention's delivery mode does not match the
    hook event — e.g. a ``context`` intervention claimed on ``PreToolUse`` —
    so the caller knows to treat the claim as a no-op.
    """
    if not isinstance(iv, dict):
        return None
    mode = iv.get("delivery_mode", "")
    message = iv.get("message", "")
    if not isinstance(message, str) or not message:
        return None
    if hook_event == "PreToolUse":
        if mode in (MODE_INTERRUPT, MODE_BOTH):
            return {"decision": "block", "reason": message}
        return None
    if hook_event == "UserPromptSubmit":
        if mode in (MODE_CONTEXT, MODE_BOTH):
            return {"hookSpecificOutput": {"additionalContext": message}}
        return None
    return None


def handle_hook(
    *,
    server_base: str,
    hook_event: str,
    input_data: dict[str, Any],
    timeout: float = _DEFAULT_TIMEOUT_SECONDS,
) -> dict[str, Any] | None:
    """Top-level entry point called by ``send_event.py``.

    Tries to claim an intervention for the given hook event and, on success,
    formats the decision JSON and reports delivery. Returns the decision
    dict that should be written to stdout, or ``None`` when there is no
    intervention to deliver.
    """
    if hook_event not in CLAIMABLE_HOOKS:
        return None
    if not isinstance(input_data, dict):
        return None
    session_id = str(input_data.get("session_id") or "")
    if not session_id:
        return None
    turn_id = str(input_data.get("turn_id") or "")

    iv = claim_intervention(
        server_base=server_base,
        session_id=session_id,
        turn_id=turn_id,
        hook_event=hook_event,
        timeout=timeout,
    )
    if iv is None:
        return None

    decision = decision_json(iv, hook_event)
    if decision is None:
        # The collector handed us a row whose mode does not match this hook.
        # Log and bail so the hook stays transparent; the sweeper will
        # eventually expire the claimed row.
        print(
            "apogee_intervention: claimed intervention does not match hook mode",
            file=sys.stderr,
        )
        return None

    mark_delivered(
        server_base=server_base,
        intervention_id=str(iv.get("intervention_id") or ""),
        hook_event=hook_event,
        timeout=timeout,
    )
    return decision


def _strip_events_suffix(url: str) -> str:
    """Normalise the caller's APOGEE_SERVER_URL to a base.

    The legacy ``send_event.py`` CLI accepts a full ``/v1/events`` URL. The
    intervention endpoints live alongside it, so we strip the trailing
    ``/v1/events`` segment to recover the collector base URL.
    """
    if not url:
        return ""
    url = url.rstrip("/")
    for suffix in ("/v1/events", "/v1/events/"):
        if url.endswith(suffix):
            return url[: -len(suffix)]
    return url


__all__ = [
    "CLAIMABLE_HOOKS",
    "MODE_BOTH",
    "MODE_CONTEXT",
    "MODE_INTERRUPT",
    "claim_intervention",
    "decision_json",
    "handle_hook",
    "mark_delivered",
]
