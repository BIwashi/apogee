"""Unit tests for apogee_hook.

These are deliberately stdlib-only so the Go CI can run
``python3 -m unittest discover hooks/tests`` without provisioning pytest.
"""

from __future__ import annotations

import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

_HOOKS_DIR = Path(__file__).resolve().parent.parent
if str(_HOOKS_DIR) not in sys.path:
    sys.path.insert(0, str(_HOOKS_DIR))

import apogee_hook  # noqa: E402
import send_event as send_event_cli  # noqa: E402


class ExtractTopLevelFieldsTest(unittest.TestCase):
    def test_pre_tool_use_flattens_tool_name_and_id(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "PreToolUse",
            {"tool_name": "Bash", "tool_use_id": "tu-1", "extra": "ignored"},
        )
        self.assertEqual(out, {"tool_name": "Bash", "tool_use_id": "tu-1"})

    def test_post_tool_use_failure_flattens_error(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "PostToolUseFailure",
            {
                "tool_name": "Read",
                "tool_use_id": "tu-2",
                "error": "ENOENT",
                "is_interrupt": False,
            },
        )
        self.assertEqual(
            out,
            {
                "tool_name": "Read",
                "tool_use_id": "tu-2",
                "error": "ENOENT",
                "is_interrupt": False,
            },
        )

    def test_permission_request_flattens_suggestions(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "PermissionRequest",
            {"tool_name": "Bash", "permission_suggestions": ["allow once"]},
        )
        self.assertEqual(
            out, {"tool_name": "Bash", "permission_suggestions": ["allow once"]}
        )

    def test_notification_flattens_notification_type(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "Notification",
            {"notification_type": "user-input", "reason": "permission required"},
        )
        self.assertEqual(
            out, {"notification_type": "user-input", "reason": "permission required"}
        )

    def test_session_start_flattens_source(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "SessionStart", {"source": "startup", "agent_type": "orchestrator"}
        )
        self.assertEqual(out, {"source": "startup", "agent_type": "orchestrator"})

    def test_session_end_flattens_reason(self) -> None:
        out = apogee_hook.extract_top_level_fields("SessionEnd", {"reason": "clear"})
        self.assertEqual(out, {"reason": "clear"})

    def test_subagent_start_flattens_agent_fields(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "SubagentStart",
            {"agent_id": "sub-1", "agent_type": "Explore"},
        )
        self.assertEqual(out, {"agent_id": "sub-1", "agent_type": "Explore"})

    def test_subagent_stop_flattens_transcript_and_stop_hook_active(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "SubagentStop",
            {
                "agent_id": "sub-1",
                "agent_transcript_path": "/tmp/sub-1.jsonl",
                "stop_hook_active": True,
            },
        )
        self.assertEqual(
            out,
            {
                "agent_id": "sub-1",
                "agent_transcript_path": "/tmp/sub-1.jsonl",
                "stop_hook_active": True,
            },
        )

    def test_user_prompt_submit_flattens_prompt(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "UserPromptSubmit", {"prompt": "hello"}
        )
        self.assertEqual(out, {"prompt": "hello"})

    def test_pre_compact_flattens_custom_instructions(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "PreCompact", {"custom_instructions": "keep tool_results"}
        )
        self.assertEqual(out, {"custom_instructions": "keep tool_results"})

    def test_unknown_keys_are_not_forwarded(self) -> None:
        out = apogee_hook.extract_top_level_fields(
            "PreToolUse", {"unknown_field": "value", "tool_name": "Edit"}
        )
        self.assertEqual(out, {"tool_name": "Edit"})

    def test_non_dict_returns_empty(self) -> None:
        self.assertEqual(apogee_hook.extract_top_level_fields("PreToolUse", []), {})  # type: ignore[arg-type]


class BuildEventTest(unittest.TestCase):
    def test_build_event_uses_session_id_from_input(self) -> None:
        ev = apogee_hook.build_event(
            source_app="test-app",
            hook_event_type="PreToolUse",
            input_data={
                "session_id": "sess-xyz",
                "tool_name": "Bash",
                "tool_use_id": "tu-1",
                "command": "ls",
            },
        )
        self.assertEqual(ev["source_app"], "test-app")
        self.assertEqual(ev["session_id"], "sess-xyz")
        self.assertEqual(ev["hook_event_type"], "PreToolUse")
        self.assertEqual(ev["tool_name"], "Bash")
        self.assertEqual(ev["tool_use_id"], "tu-1")
        self.assertIn("timestamp", ev)
        self.assertIsInstance(ev["timestamp"], int)
        self.assertGreater(ev["timestamp"], 0)
        self.assertEqual(ev["payload"]["command"], "ls")

    def test_build_event_defaults_session_id_to_unknown(self) -> None:
        ev = apogee_hook.build_event(
            source_app="test-app",
            hook_event_type="Stop",
            input_data={},
        )
        self.assertEqual(ev["session_id"], "unknown")

    def test_build_event_preserves_summary_and_chat(self) -> None:
        ev = apogee_hook.build_event(
            source_app="a",
            hook_event_type="Stop",
            input_data={"session_id": "s"},
            summary="done",
            chat=[{"role": "user"}],
        )
        self.assertEqual(ev["summary"], "done")
        self.assertEqual(ev["chat"], [{"role": "user"}])


class SendEventTest(unittest.TestCase):
    def test_send_event_builds_correct_payload(self) -> None:
        captured: dict = {}

        class _Resp:
            status = 200

            def __enter__(self_inner):
                return self_inner

            def __exit__(self_inner, *exc):
                return False

        def fake_urlopen(req, timeout):
            captured["url"] = req.full_url
            captured["body"] = req.data
            captured["timeout"] = timeout
            captured["content_type"] = req.headers.get("Content-type")
            return _Resp()

        with mock.patch.object(apogee_hook.urllib.request, "urlopen", side_effect=fake_urlopen):
            apogee_hook.send_event(
                source_app="unit",
                hook_event_type="PreToolUse",
                input_data={
                    "session_id": "sess-1",
                    "tool_name": "Bash",
                    "tool_use_id": "tu-1",
                    "command": "ls",
                },
                server_url="http://localhost:4100/v1/events",
                timeout=1.5,
            )

        self.assertEqual(captured["url"], "http://localhost:4100/v1/events")
        self.assertEqual(captured["content_type"], "application/json")
        self.assertEqual(captured["timeout"], 1.5)
        body = json.loads(captured["body"])
        self.assertEqual(body["source_app"], "unit")
        self.assertEqual(body["session_id"], "sess-1")
        self.assertEqual(body["hook_event_type"], "PreToolUse")
        self.assertEqual(body["tool_name"], "Bash")
        self.assertEqual(body["tool_use_id"], "tu-1")
        self.assertEqual(body["payload"]["command"], "ls")

    def test_send_event_silent_on_network_failure(self) -> None:
        def fake_urlopen(req, timeout):
            raise urllib.error.URLError("connection refused")

        stderr = io.StringIO()
        with mock.patch.object(apogee_hook.urllib.request, "urlopen", side_effect=fake_urlopen), \
             mock.patch.object(sys, "stderr", stderr):
            # Must not raise.
            result = apogee_hook.send_event(
                source_app="unit",
                hook_event_type="Stop",
                input_data={"session_id": "sess-1"},
            )
        self.assertIsNone(result)
        self.assertIn("network error", stderr.getvalue())


class SendEventCliTest(unittest.TestCase):
    def test_cli_echoes_stdin_and_posts_event(self) -> None:
        payload = json.dumps(
            {"session_id": "sess-cli", "tool_name": "Read", "file_path": "README.md"}
        )

        stdin_buf = io.StringIO(payload)
        stdout_buf = io.StringIO()

        posted: dict = {}

        def fake_send_event(**kwargs):
            posted.update(kwargs)

        with mock.patch.object(sys, "stdin", stdin_buf), \
             mock.patch.object(sys, "stdout", stdout_buf), \
             mock.patch.object(send_event_cli.apogee_hook, "send_event", side_effect=fake_send_event):
            rc = send_event_cli.main(
                [
                    "--source-app",
                    "cli-test",
                    "--event-type",
                    "PreToolUse",
                    "--server-url",
                    "http://localhost:9999/v1/events",
                ]
            )

        self.assertEqual(rc, 0)
        self.assertEqual(stdout_buf.getvalue(), payload)
        self.assertEqual(posted["source_app"], "cli-test")
        self.assertEqual(posted["hook_event_type"], "PreToolUse")
        self.assertEqual(posted["server_url"], "http://localhost:9999/v1/events")
        self.assertEqual(posted["input_data"]["session_id"], "sess-cli")


class DeriveSourceAppTest(unittest.TestCase):
    def _fake_run(self, returncode: int, stdout: str = "") -> mock.Mock:
        cp = subprocess.CompletedProcess(
            args=["git", "rev-parse", "--show-toplevel"],
            returncode=returncode,
            stdout=stdout,
            stderr="",
        )
        return mock.Mock(return_value=cp)

    def test_env_var_wins(self) -> None:
        with mock.patch.dict(os.environ, {"APOGEE_SOURCE_APP": "from-env"}, clear=False):
            self.assertEqual(apogee_hook.derive_source_app(), "from-env")

    def test_env_var_empty_falls_through(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = Path(tmp) / "my-repo"
            workdir.mkdir()
            original = Path.cwd()
            os.chdir(workdir)
            try:
                with mock.patch.dict(os.environ, {"APOGEE_SOURCE_APP": "  "}, clear=False), \
                     mock.patch.object(
                         subprocess,
                         "run",
                         self._fake_run(returncode=128),
                     ):
                    self.assertEqual(apogee_hook.derive_source_app(), "my-repo")
            finally:
                os.chdir(original)

    def test_git_toplevel_basename(self) -> None:
        with mock.patch.dict(os.environ, {}, clear=True), \
             mock.patch.object(
                 subprocess,
                 "run",
                 self._fake_run(returncode=0, stdout="/Users/me/work/apogee\n"),
             ):
            self.assertEqual(apogee_hook.derive_source_app(), "apogee")

    def test_cwd_fallback_when_git_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = Path(tmp) / "standalone-project"
            workdir.mkdir()
            original = Path.cwd()
            os.chdir(workdir)
            try:
                with mock.patch.dict(os.environ, {}, clear=True), \
                     mock.patch.object(
                         subprocess,
                         "run",
                         self._fake_run(returncode=128),
                     ):
                    self.assertEqual(
                        apogee_hook.derive_source_app(), "standalone-project"
                    )
            finally:
                os.chdir(original)

    def test_git_missing_binary_falls_back(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = Path(tmp) / "no-git-here"
            workdir.mkdir()
            original = Path.cwd()
            os.chdir(workdir)
            try:
                with mock.patch.dict(os.environ, {}, clear=True), \
                     mock.patch.object(
                         subprocess,
                         "run",
                         side_effect=FileNotFoundError("git not installed"),
                     ):
                    self.assertEqual(
                        apogee_hook.derive_source_app(), "no-git-here"
                    )
            finally:
                os.chdir(original)


class SendEventCliDynamicSourceAppTest(unittest.TestCase):
    def test_cli_without_source_app_uses_derive(self) -> None:
        payload = json.dumps({"session_id": "sess-dyn", "tool_name": "Bash"})
        stdin_buf = io.StringIO(payload)
        stdout_buf = io.StringIO()

        posted: dict = {}

        def fake_send_event(**kwargs):
            posted.update(kwargs)

        with mock.patch.object(sys, "stdin", stdin_buf), \
             mock.patch.object(sys, "stdout", stdout_buf), \
             mock.patch.object(send_event_cli.apogee_hook, "send_event", side_effect=fake_send_event), \
             mock.patch.object(send_event_cli.apogee_hook, "derive_source_app", return_value="derived-app"):
            rc = send_event_cli.main(
                [
                    "--event-type",
                    "PreToolUse",
                    "--server-url",
                    "http://localhost:9999/v1/events",
                ]
            )

        self.assertEqual(rc, 0)
        self.assertEqual(posted["source_app"], "derived-app")

    def test_cli_explicit_source_app_overrides_derive(self) -> None:
        payload = json.dumps({"session_id": "sess-explicit", "tool_name": "Read"})
        stdin_buf = io.StringIO(payload)
        stdout_buf = io.StringIO()

        posted: dict = {}

        def fake_send_event(**kwargs):
            posted.update(kwargs)

        with mock.patch.object(sys, "stdin", stdin_buf), \
             mock.patch.object(sys, "stdout", stdout_buf), \
             mock.patch.object(send_event_cli.apogee_hook, "send_event", side_effect=fake_send_event), \
             mock.patch.object(
                 send_event_cli.apogee_hook,
                 "derive_source_app",
                 side_effect=AssertionError("should not be called when --source-app is set"),
             ):
            rc = send_event_cli.main(
                [
                    "--source-app",
                    "explicit",
                    "--event-type",
                    "PostToolUse",
                    "--server-url",
                    "http://localhost:9999/v1/events",
                ]
            )

        self.assertEqual(rc, 0)
        self.assertEqual(posted["source_app"], "explicit")


if __name__ == "__main__":
    unittest.main()
