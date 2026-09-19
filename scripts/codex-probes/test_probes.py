"""Offline regression tests; fake executable only, never a real provider or Codex."""

import argparse
import asyncio
from datetime import datetime, timedelta, timezone, time as wallclock
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import keepalive
from probe_common import ERROR_LIMIT, Events, HIGH_DEMAND, Outcome, OwnedState, Questions, Runner, StderrErrors, atomic_json, load_bank, process_identity, redact_error
from scheduled_probe import choose_slot
from quota_schedule import daily_slots, weekly_reset
from zoneinfo import ZoneInfo
from pause_control import PauseControl


CHINESE_CAPACITY = "当前模型 gpt-6-astra 负载已经达到上限，请稍后重试 (request id: synthetic-request-id)"


FAKE = '''#!/usr/bin/env python3
import json, os, pathlib, signal, sys, time
if '--version' in sys.argv:
    print('codex-cli 0.155.1')
    sys.exit(0)
assert sys.argv[1] == 'exec' and '--json' in sys.argv and '--ephemeral' in sys.argv
home = pathlib.Path(os.environ['CODEX_HOME'])
assert home.name == 'codex-home' and home.parent.name.startswith('attempt-')
assert os.environ['AXONHUB_ISOLATED_PROBE_KEY'] == 'synthetic-test-key'
assert 'synthetic-test-key' not in (home / 'config.toml').read_text()
assert 'OPENAI_API_KEY' not in os.environ
(home / 'session-private.txt').write_text('private')
if os.environ.get('FAKE_COUNT'):
    with open(os.environ['FAKE_COUNT'], 'a') as f: f.write('attempt\\n')
mode = os.environ.get('FAKE_MODE')
if os.environ.get('FAKE_SEQUENCE'):
    index = len(pathlib.Path(os.environ['FAKE_COUNT']).read_text().splitlines()) - 1
    mode = os.environ['FAKE_SEQUENCE'].split(',')[index]
if mode == 'startup-error':
    print('Error: invalid config; api_key=synthetic-test-key', file=sys.stderr, flush=True)
    sys.exit(1)
if mode == 'stderr-demand':
    print("Error: We're currently experiencing high demand, which may cause temporary errors", file=sys.stderr, flush=True)
    sys.exit(1)
assert sys.stdin.read()
print(json.dumps({'type':'thread.started','thread_id':'owned-fake-thread'}), flush=True)
if os.environ.get('FAKE_READY'):
    pathlib.Path(os.environ['FAKE_READY']).write_text(str(os.getpid()))
if mode == 'sleep':
    signal.signal(signal.SIGINT, lambda *_: sys.exit(0))
    time.sleep(600)
if mode == 'demand':
    print(json.dumps({'type':'turn.failed','error':{'message':"We're currently experiencing high demand, which may cause temporary errors"}}), flush=True)
    sys.exit(1)
if mode in ('demand-curly', 'http-500'):
    message = "We’re currently experiencing high demand, which may cause temporary errors." if mode == 'demand-curly' else 'unexpected status 500 Internal Server Error: upstream internal error'
    print(json.dumps({'type':'turn.failed','error':{'message':message}}), flush=True)
    sys.exit(1)
if mode == 'demand-cn':
    body = json.dumps({'error': {'message': '当前模型 gpt-6-astra 负载已经达到上限，请稍后重试 (request id: synthetic-request-id)'}}, ensure_ascii=False)
    message = 'unexpected status 500 Internal Server Error: ' + body
    print(json.dumps({'type':'error','message':message}), flush=True)
    print(json.dumps({'type':'turn.failed','error':{'message':message}}), flush=True)
    sys.exit(1)
if mode == 'private-error':
    print(json.dumps({'type':'turn.failed','error':{'message':'invalid API key: synthetic-test-key; api_key=other-private-key; Authorization: Bearer header-private-key'}}), flush=True)
    print('Error: later stderr must not replace the terminal error', file=sys.stderr, flush=True)
    sys.exit(1)
print(json.dumps({'type':'turn.completed','usage':{'input_tokens':12,'output_tokens':3,'reasoning_output_tokens':2}}), flush=True)
'''


def args_for(root, executable):
    return argparse.Namespace(url="http://127.0.0.1:1/v1", model="fake-model", key_env="FAKE_KEY",
        codex=str(executable), timeout=5, max_attempts=0, max_tokens=0,
        supports_websockets=False, effort="low", state_dir=root)


class EventsTest(unittest.TestCase):
    def feed(self, events, kind, **data):
        events.feed(json.dumps(dict(type=kind, **data)).encode())

    def test_success_overrides_intermediate_retry_message(self):
        events = Events()
        self.feed(events, "error", message=HIGH_DEMAND)
        self.feed(events, "turn.completed", usage={"input_tokens": 3})
        self.assertEqual(events.outcome(0).status, "success")

    def test_agent_quoted_error_and_partial_exit_are_not_success(self):
        for message in (HIGH_DEMAND, CHINESE_CAPACITY):
            with self.subTest(message=message):
                events = Events()
                self.feed(events, "item.completed", item={"type": "agent_message", "text": message})
                self.assertEqual(events.outcome(0).status, "error")

    def test_only_last_structured_error_controls_retry(self):
        events = Events()
        self.feed(events, "error", message=HIGH_DEMAND)
        self.feed(events, "turn.failed", error={"message": "invalid API key"})
        self.assertEqual(events.outcome(1).status, "error")
        self.feed(events, "turn.failed", error={"message": HIGH_DEMAND})
        self.assertEqual(events.outcome(1).status, "high_demand")

    def test_observed_capacity_in_http_error_envelope(self):
        events = Events()
        message = "unexpected status 500 Internal Server Error: " + json.dumps({"error": {"message": CHINESE_CAPACITY}}, ensure_ascii=False)
        self.feed(events, "turn.failed", error={"message": message})
        result = events.outcome(1)
        self.assertEqual(result.status, "high_demand")
        self.assertIn(CHINESE_CAPACITY, result.error)
        self.assertEqual(result.error_source, "turn.failed")

    def test_terminal_failure_has_priority_over_late_error(self):
        for terminal, late, expected in (("invalid API key", CHINESE_CAPACITY, "error"),
                                         (CHINESE_CAPACITY, "stream disconnected", "high_demand"),
                                         (None, HIGH_DEMAND, "error")):
            with self.subTest(terminal=terminal):
                events = Events()
                self.feed(events, "error", message=HIGH_DEMAND)
                self.feed(events, "turn.failed", error={"message": terminal})
                self.feed(events, "error", message=late)
                self.assertEqual(events.outcome(1).status, expected)

    def test_other_http_and_quota_errors_are_not_capacity(self):
        for message in ("unexpected status 429 Too Many Requests", "the request contains 500 items",
                        "insufficient_quota", "模型不存在", "account banned: " + HIGH_DEMAND, "账号封禁 " + CHINESE_CAPACITY,
                        "unexpected status 401 Unauthorized: previous error was " + HIGH_DEMAND,
                        "unexpected status 403: " + CHINESE_CAPACITY):
            with self.subTest(message=message):
                events = Events()
                self.feed(events, "turn.failed", error={"message": message})
                self.assertEqual(events.outcome(1).status, "error")

    def test_high_demand_accepts_typographic_apostrophes(self):
        for apostrophe in ("'", "’", "‘", "ʼ", "＇"):
            with self.subTest(apostrophe=apostrophe):
                events = Events()
                self.feed(events, "turn.failed", error={"message": HIGH_DEMAND.replace("'", apostrophe) + "."})
                self.assertEqual(events.outcome(1).status, "high_demand")

    def test_http_500_is_distinct_from_capacity_and_other_numbers(self):
        for message in ("unexpected status 500 Internal Server Error: unknown provider failure",
                        "HTTP 500: internal error", "HTTP/1.1 500 Internal Server Error"):
            with self.subTest(message=message):
                events = Events()
                self.feed(events, "turn.failed", error={"message": message})
                result = events.outcome(1)
                self.assertEqual(result.status, "http_500")
                self.assertEqual(result.http_status, 500)
                self.assertEqual(result.error, message)

    def test_error_diagnostics_redact_before_truncation(self):
        secret = "synthetic/test+key"
        messages = (
            f"Error: key was {secret}; token=private-token; api_key=private-api-key",
            'Error: {"access_token":"private-access-token","password":"private-password"}',
            "Error: Authorization: Bearer private-header-key; Cookie: private-cookie",
            "Error: https://private-user:private-password@example.test/v1?key=private-query-key",
            "Error: synthetic%2Ftest%2Bkey",
            "x" * (ERROR_LIMIT - 5) + secret,
        )
        for message in messages:
            with self.subTest(message=message[:60]):
                result = redact_error(message, secret)
                self.assertLessEqual(len(result), ERROR_LIMIT)
                self.assertNotIn("private-", result)
                self.assertNotIn(secret, result)
                self.assertNotIn("synthetic%2Ftest%2Bkey", result)
                self.assertTrue("[REDACTED" in result or result.endswith("..."))

    def test_stderr_errors_are_bounded_and_handle_chunked_keys(self):
        errors = StderrErrors("synthetic-test-key")
        errors.feed(b"prompt or warning must not be logged\nError: config synthetic-")
        errors.feed(b"test-key\n")
        self.assertEqual(errors.last_error, "Error: config [REDACTED]")
        errors.feed(b"Error: " + b"x" * 17000)
        self.assertEqual(errors.pending, b"")
        errors.feed(b"secret suffix must not be logged\n")
        errors.feed(b"Error: token=private-token")
        errors.finish()
        self.assertEqual(errors.last_error, "Error: token=[REDACTED]")

    def test_banks_are_valid(self):
        here = Path(__file__).parent
        for name, minimum in (("questions-simple.json", 1000), ("questions-reasoning.json", 100)):
            entries = load_bank(here / name)
            self.assertGreaterEqual(len(entries), minimum)
            self.assertEqual(len({entry['prompt'] for entry in entries}), len(entries))


class OwnershipTest(unittest.TestCase):
    def test_cleanup_never_follows_job_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            outside = base / "normal-codex-session"
            outside.mkdir()
            (outside / "keep.txt").write_text("keep")
            with OwnedState(base / "owned") as state:
                job, _ = state.create_job()
                (job / "link").symlink_to(outside, target_is_directory=True)
                state.remove_job(job)
                self.assertEqual((outside / "keep.txt").read_text(), "keep")
                job = state.root / ("attempt-" + "f" * 32)
                job.symlink_to(outside, target_is_directory=True)
                with self.assertRaises(ValueError):
                    state.remove_job(job)

    def test_unknown_directory_and_wrong_owner_refused(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            (base / "normal-session").write_text("keep")
            with self.assertRaises(ValueError):
                with OwnedState(base):
                    pass
            with OwnedState(base / "owned") as state:
                job, marker = state.create_job()
                marker["owner"] = "wrong"
                atomic_json(job / ".job.json", marker)
                with self.assertRaises(ValueError):
                    state.remove_job(job)
                self.assertTrue(job.exists())

    def test_singleton_lock(self):
        with tempfile.TemporaryDirectory() as tmp:
            with OwnedState(Path(tmp) / "owned"):
                with self.assertRaises(BlockingIOError):
                    with OwnedState(Path(tmp) / "owned"):
                        pass


class ProcessTest(unittest.IsolatedAsyncioTestCase):
    async def test_pause_waits_for_cleanup_and_resume_does_not_reuse_turn(self):
        started = asyncio.Event()
        cleaned = asyncio.Event()
        cleanup_allowed = asyncio.Event()
        with PauseControl() as control:
            async def work():
                started.set()
                try:
                    await asyncio.sleep(100)
                finally:
                    await cleanup_allowed.wait()
                    cleaned.set()
            task = asyncio.create_task(control.run(work))
            await started.wait()
            control.request_pause()
            await asyncio.sleep(0)
            self.assertFalse(task.done())
            self.assertFalse(cleaned.is_set())
            control.request_resume()  # Even early resume must wait for cleanup.
            cleanup_allowed.set()
            self.assertIsNone(await task)
            self.assertTrue(cleaned.is_set())
            self.assertEqual(await control.run(lambda: asyncio.sleep(0, result='new')), 'new')

    async def asyncSetUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.base = Path(self.tmp.name)
        self.fake = self.base / "fake-codex"
        self.fake.write_text(FAKE)
        self.fake.chmod(0o700)
        self.args = args_for(self.base / "owned", self.fake)

    async def asyncTearDown(self):
        self.tmp.cleanup()

    async def test_success_is_isolated_and_cleaned(self):
        with patch.dict(os.environ, {"OPENAI_API_KEY": "must-not-inherit"}):
            with OwnedState(self.args.state_dir) as state:
                runner = Runner(self.args, state, "synthetic-test-key")
                await runner.check_codex()
                result = await runner.run({"id": "q", "prompt": "one question"}, "low")
                self.assertEqual(result.status, "success")
                self.assertEqual(runner.reported_tokens, 15)  # reasoning is a subset, not added again
                self.assertFalse(list(state.root.glob("attempt-*")))

    async def test_cancellation_reaps_before_cleanup(self):
        ready = self.base / "ready"
        with patch.dict(os.environ, {"FAKE_MODE": "sleep", "FAKE_READY": str(ready)}):
            with OwnedState(self.args.state_dir) as state:
                task = asyncio.create_task(Runner(self.args, state, "synthetic-test-key").run({"id": "q", "prompt": "q"}, "low"))
                for _ in range(100):
                    if ready.exists():
                        break
                    await asyncio.sleep(0.02)
                self.assertTrue(ready.exists())
                pid = int(ready.read_text())
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task
                self.assertIsNone(process_identity(pid))
                self.assertFalse(list(state.root.glob("attempt-*")))

    async def test_failure_logs_are_useful_redacted_and_cleaned(self):
        for mode, status, source, fragment in (
            ("demand-cn", "high_demand", "turn.failed", "负载已经达到上限"),
            ("demand-curly", "high_demand", "turn.failed", "We’re"),
            ("http-500", "http_500", "turn.failed", "500"),
            ("private-error", "error", "turn.failed", "invalid API key"),
            ("startup-error", "error", "stderr", "invalid config"),
            ("stderr-demand", "error", "stderr", "high demand"),
        ):
            with self.subTest(mode=mode), patch.dict(os.environ, {"FAKE_MODE": mode}):
                with OwnedState(self.args.state_dir) as state, patch("probe_common.emit") as log:
                    result = await Runner(self.args, state, "synthetic-test-key").run({"id": "q", "prompt": "q" * 16000}, "low")
                    self.assertEqual(result.status, status)
                    self.assertEqual(result.error_source, source)
                    self.assertIn(fragment, result.error)
                    self.assertFalse(list(state.root.glob("attempt-*")))
                    record = log.call_args.kwargs
                    self.assertEqual(log.call_args.args[0], "attempt_result")
                    self.assertEqual(record["error"], result.error)
                    rendered = json.dumps(record)
                    for secret in ("synthetic-test-key", "other-private-key", "header-private-key"):
                        self.assertNotIn(secret, rendered)

    async def test_timeout_cleans(self):
        self.args.timeout = 0.3
        with patch.dict(os.environ, {"FAKE_MODE": "sleep"}):
            with OwnedState(self.args.state_dir) as state:
                result = await Runner(self.args, state, "synthetic-test-key").run({"id": "q", "prompt": "q"}, "low")
                self.assertEqual(result.status, "timeout")
                self.assertFalse(list(state.root.glob("attempt-*")))

    async def test_stale_pid_does_not_kill_unrelated_process(self):
        with OwnedState(self.args.state_dir) as state:
            job, marker = state.create_job()
            marker.update(pid=os.getpid(), identity="different-boot:1")
            atomic_json(job / ".job.json", marker)
            await state.recover()
            self.assertFalse(job.exists())

    async def test_acquire_cleans_siblings_before_return(self):
        active = set()
        class FakeRunner:
            def budget_exhausted(self):
                return False

            async def run(self, question, effort):
                token = object()
                active.add(token)
                try:
                    if len(active) == 2:
                        return Outcome("success")
                    await asyncio.sleep(20)
                finally:
                    active.remove(token)
        args = argparse.Namespace(stagger=0.001, retry_min=0.001, retry_max=0.01, concurrency=2, effort="low")
        result = await keepalive.acquire(FakeRunner(), Questions([{"id": "q", "prompt": "q"}]), args)
        self.assertEqual(result, "success")
        self.assertEqual(active, set())


class ScheduleTest(unittest.TestCase):
    def test_two_daily_slots_are_five_hours_apart(self):
        now = datetime(2026, 9, 19, 2, 0, tzinfo=timezone.utc)
        slots = daily_slots(now, wallclock(8), ZoneInfo('Asia/Shanghai'))
        today = [(key, at) for key, at in slots if key.startswith('daily:2026-09-19:')]
        self.assertEqual(len(today), 2)
        self.assertEqual(today[1][1] - today[0][1], timedelta(hours=5))
        self.assertEqual(today[0][1].hour, 0)

    def test_weekly_uses_normalized_window_not_top_level_reset(self):
        now = datetime(2026, 9, 19, 2, 0, tzinfo=timezone.utc)
        snapshot = {'observed_at': now.isoformat(), 'server_time': now.isoformat(),
                    'windows': [{'window': '5h', 'next_reset_at': (now + timedelta(hours=5)).isoformat()},
                                {'window': '7d', 'next_reset_at': (now + timedelta(days=4)).isoformat()}]}
        self.assertEqual(weekly_reset(snapshot, now, 3600), now + timedelta(days=4))
        snapshot['observed_at'] = (now - timedelta(hours=2)).isoformat()
        self.assertIsNone(weekly_reset(snapshot, now, 3600))
        snapshot['observed_at'] = now.isoformat()
        snapshot['windows'].append({'window': 'weekly', 'next_reset_at': (now + timedelta(days=3)).isoformat()})
        with self.assertRaises(ValueError):
            weekly_reset(snapshot, now, 3600)

    def test_keepalive_cumulative_ten_errors_reenter_phase_one(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fake = root / "fake-codex"
            fake.write_text(FAKE)
            fake.chmod(0o700)
            count = root / "calls"
            sequence = ["demand-cn"] + ["http-500"] * 3 + ["demand-curly", "success"] + ["demand", "success", "demand-cn", "success"] * 4 + ["demand-curly", "success", "demand-cn", "success"]
            env = dict(os.environ, FAKE_COUNT=str(count), FAKE_SEQUENCE=','.join(sequence),
                       AXONHUB_PROBE_API_KEY="synthetic-test-key")
            command = [sys.executable, str(Path(__file__).with_name("keepalive.py")),
                "--url", "http://127.0.0.1:1/v1", "--model", "fake-model", "--codex", str(fake),
                "--state-dir", str(root / "state"), "--concurrency", "1", "--max-attempts", str(len(sequence)),
                "--stagger", "0.001", "--retry-min", "0.001", "--retry-max", "0.002",
                "--interval-min", "0.001", "--interval-max", "0.002"]
            result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            events = [json.loads(line) for line in result.stdout.splitlines()]
            phases = [e['phase'] for e in events if e['event'] == 'phase']
            self.assertEqual(phases.count(1), 2)
            attempts = [e for e in events if e['event'] == 'attempt_result']
            self.assertEqual(attempts[0]['status'], 'high_demand')
            self.assertIn('负载已经达到上限', attempts[0]['error'])
            self.assertEqual([e['status'] for e in attempts[1:4]], ['http_500'] * 3)
            self.assertEqual(attempts[4]['status'], 'high_demand')
            progress = json.loads((root / 'state/progress.json').read_text())
            self.assertEqual((progress['phase'], progress['high_demand']), (2, 0))
            self.assertEqual(len(count.read_text().splitlines()), len(sequence))
            self.assertFalse(list((root / 'state').glob('attempt-*')))

    def test_weekly_and_daily_timezone(self):
        now = datetime(2026, 9, 19, 2, 0, tzinfo=timezone.utc)
        args = argparse.Namespace(now=False, at=None, daily=None, weekly="Mon@08:00", timezone="Asia/Shanghai")
        slot, _ = choose_slot(args, now)
        self.assertEqual(slot.isoformat(), "2026-09-21T08:00:00+08:00")
        args.daily = "08:00"
        slot, _ = choose_slot(args, now)
        self.assertEqual(slot.isoformat(), "2026-09-20T08:00:00+08:00")

    def test_naive_at_rejected(self):
        args = argparse.Namespace(now=False, at="2026-09-21T08:00", daily=None, weekly=None)
        with self.assertRaises(ValueError):
            choose_slot(args, datetime.now(timezone.utc))

    def test_same_slot_is_not_replayed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fake = root / "fake-codex"
            fake.write_text(FAKE)
            fake.chmod(0o700)
            count = root / "calls"
            env = dict(os.environ, FAKE_COUNT=str(count), AXONHUB_PROBE_API_KEY="synthetic-test-key")
            command = [sys.executable, str(Path(__file__).with_name("scheduled_probe.py")),
                "--url", "http://127.0.0.1:1/v1", "--model", "fake-model", "--codex", str(fake),
                "--state-dir", str(root / "state"), "--at", "2020-01-01T00:00:00Z"]
            for _ in range(2):
                result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(count.read_text(), "attempt\n")
            self.assertFalse(list((root / "state").glob("attempt-*")))


if __name__ == "__main__":
    unittest.main()
