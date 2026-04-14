"""Unit tests for apogee_intervention."""

from __future__ import annotations

import io
import json
import sys
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

_HOOKS_DIR = Path(__file__).resolve().parent.parent
if str(_HOOKS_DIR) not in sys.path:
    sys.path.insert(0, str(_HOOKS_DIR))

import apogee_intervention  # noqa: E402


class _FakeResp:
    def __init__(self, body: bytes, status: int = 200) -> None:
        self._body = body
        self.status = status

    def read(self) -> bytes:
        return self._body

    def close(self) -> None:
        pass

    def __enter__(self) -> "_FakeResp":
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        return None


def _intervention(**overrides) -> dict:
    base = {
        "intervention_id": "intv-aabbccddeeff",
        "session_id": "sess-1",
        "turn_id": "turn-1",
        "message": "stop and reconsider",
        "delivery_mode": "interrupt",
        "scope": "this_turn",
        "urgency": "normal",
        "status": "claimed",
    }
    base.update(overrides)
    return base


class DecisionJSONTest(unittest.TestCase):
    def test_interrupt_pre_tool_use(self) -> None:
        out = apogee_intervention.decision_json(
            _intervention(delivery_mode="interrupt"), "PreToolUse"
        )
        self.assertEqual(out, {"decision": "block", "reason": "stop and reconsider"})

    def test_interrupt_on_user_prompt_submit_is_none(self) -> None:
        # interrupt mode does not match the context path.
        out = apogee_intervention.decision_json(
            _intervention(delivery_mode="interrupt"), "UserPromptSubmit"
        )
        self.assertIsNone(out)

    def test_context_user_prompt_submit(self) -> None:
        out = apogee_intervention.decision_json(
            _intervention(delivery_mode="context"), "UserPromptSubmit"
        )
        self.assertEqual(
            out,
            {"hookSpecificOutput": {"additionalContext": "stop and reconsider"}},
        )

    def test_both_works_either_way(self) -> None:
        self.assertEqual(
            apogee_intervention.decision_json(_intervention(delivery_mode="both"), "PreToolUse"),
            {"decision": "block", "reason": "stop and reconsider"},
        )
        self.assertEqual(
            apogee_intervention.decision_json(
                _intervention(delivery_mode="both"), "UserPromptSubmit"
            ),
            {"hookSpecificOutput": {"additionalContext": "stop and reconsider"}},
        )

    def test_unknown_hook_returns_none(self) -> None:
        self.assertIsNone(
            apogee_intervention.decision_json(_intervention(), "PostToolUse")
        )

    def test_empty_message_returns_none(self) -> None:
        self.assertIsNone(apogee_intervention.decision_json(_intervention(message=""), "PreToolUse"))


class StripEventsSuffixTest(unittest.TestCase):
    def test_strips_v1_events(self) -> None:
        self.assertEqual(
            apogee_intervention._strip_events_suffix("http://localhost:4100/v1/events"),
            "http://localhost:4100",
        )

    def test_trailing_slash(self) -> None:
        self.assertEqual(
            apogee_intervention._strip_events_suffix("http://localhost:4100/v1/events/"),
            "http://localhost:4100",
        )

    def test_already_base(self) -> None:
        self.assertEqual(
            apogee_intervention._strip_events_suffix("http://localhost:4100"),
            "http://localhost:4100",
        )


class ClaimInterventionTest(unittest.TestCase):
    def test_no_claim_returns_none_on_204(self) -> None:
        fake = _FakeResp(b"", status=204)
        with mock.patch("urllib.request.urlopen", return_value=fake):
            out = apogee_intervention.claim_intervention(
                server_base="http://localhost:4100/v1/events",
                session_id="sess-1",
                turn_id="turn-1",
                hook_event="PreToolUse",
            )
        self.assertIsNone(out)

    def test_claim_decodes_intervention(self) -> None:
        body = json.dumps({"intervention": _intervention()}).encode("utf-8")
        fake = _FakeResp(body, status=200)
        with mock.patch("urllib.request.urlopen", return_value=fake):
            out = apogee_intervention.claim_intervention(
                server_base="http://localhost:4100/v1/events",
                session_id="sess-1",
                turn_id="turn-1",
                hook_event="PreToolUse",
            )
        self.assertIsNotNone(out)
        self.assertEqual(out["intervention_id"], "intv-aabbccddeeff")

    def test_claim_skipped_for_non_claimable_hook(self) -> None:
        with mock.patch("urllib.request.urlopen") as m:
            out = apogee_intervention.claim_intervention(
                server_base="http://localhost:4100/v1/events",
                session_id="sess-1",
                turn_id="turn-1",
                hook_event="PostToolUse",
            )
        self.assertIsNone(out)
        m.assert_not_called()

    def test_claim_network_error_swallowed(self) -> None:
        def raise_url_error(*a, **kw):
            raise urllib.error.URLError("connection refused")

        with mock.patch("urllib.request.urlopen", side_effect=raise_url_error):
            out = apogee_intervention.claim_intervention(
                server_base="http://localhost:4100/v1/events",
                session_id="sess-1",
                turn_id="turn-1",
                hook_event="PreToolUse",
            )
        self.assertIsNone(out)


class HandleHookTest(unittest.TestCase):
    def test_pass_through_when_claim_returns_nothing(self) -> None:
        with mock.patch.object(apogee_intervention, "claim_intervention", return_value=None):
            out = apogee_intervention.handle_hook(
                server_base="http://localhost:4100/v1/events",
                hook_event="PreToolUse",
                input_data={"session_id": "sess-1", "turn_id": "turn-1"},
            )
        self.assertIsNone(out)

    def test_decision_emitted_and_delivered_called(self) -> None:
        delivered = []

        def fake_deliver(**kwargs):
            delivered.append(kwargs)

        with mock.patch.object(
            apogee_intervention,
            "claim_intervention",
            return_value=_intervention(delivery_mode="interrupt"),
        ), mock.patch.object(apogee_intervention, "mark_delivered", side_effect=fake_deliver):
            out = apogee_intervention.handle_hook(
                server_base="http://localhost:4100/v1/events",
                hook_event="PreToolUse",
                input_data={"session_id": "sess-1", "turn_id": "turn-1"},
            )
        self.assertEqual(out, {"decision": "block", "reason": "stop and reconsider"})
        self.assertEqual(len(delivered), 1)
        self.assertEqual(delivered[0]["intervention_id"], "intv-aabbccddeeff")

    def test_context_mode_on_user_prompt_submit(self) -> None:
        with mock.patch.object(
            apogee_intervention,
            "claim_intervention",
            return_value=_intervention(delivery_mode="context"),
        ), mock.patch.object(apogee_intervention, "mark_delivered"):
            out = apogee_intervention.handle_hook(
                server_base="http://localhost:4100/v1/events",
                hook_event="UserPromptSubmit",
                input_data={"session_id": "sess-1"},
            )
        self.assertEqual(
            out,
            {"hookSpecificOutput": {"additionalContext": "stop and reconsider"}},
        )

    def test_delivered_swallows_network_error(self) -> None:
        def raise_url_error(*a, **kw):
            raise urllib.error.URLError("no route")

        # Should not propagate — mark_delivered must never raise.
        with mock.patch("urllib.request.urlopen", side_effect=raise_url_error):
            apogee_intervention.mark_delivered(
                server_base="http://localhost:4100/v1/events",
                intervention_id="intv-1",
                hook_event="PreToolUse",
            )

    def test_claimable_hooks_only(self) -> None:
        with mock.patch.object(apogee_intervention, "claim_intervention") as m:
            out = apogee_intervention.handle_hook(
                server_base="http://localhost:4100/v1/events",
                hook_event="PostToolUse",
                input_data={"session_id": "sess-1"},
            )
        self.assertIsNone(out)
        m.assert_not_called()


if __name__ == "__main__":
    unittest.main()
