"""Opt-in hosted check: real packaged Codex, loopback mock only, synthetic key."""

import asyncio
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from probe_common import OwnedState, Runner
from test_probes import args_for


@unittest.skipUnless(os.environ.get("PROBE_TEST_CODEX"), "real CLI fixture runs only in hosted CI")
class CLIIntegrationTest(unittest.IsolatedAsyncioTestCase):
    async def test_completed_turn_preserves_jsonl_usage(self):
        await self.completed_turn()

    @unittest.skipUnless(os.environ.get("PROBE_TEST_CODEX_VERSION") == "0.162.0", "requires partial_answer support")
    async def test_partial_answer_continues_before_final_answer(self):
        await self.completed_turn(partial=True)

    @unittest.skipUnless(os.environ.get("PROBE_TEST_CODEX_VERSION") == "0.162.0", "requires streamed Retry-After support")
    async def test_gateway_retry_failure_obeys_server_delay(self):
        await self.completed_turn(retry=True)

    async def completed_turn(self, *, retry=False, partial=False):
        requests = []
        arrivals = []
        errors = []
        model = os.environ.get("PROBE_TEST_MODEL", "gpt-6-sol")
        message = {"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed",
                   "content": [{"type": "output_text", "text": "OK.", "annotations": []}]}
        response = {"id": "resp_fixture", "object": "response", "created_at": 1700000000,
                    "model": model, "status": "completed", "output": [message],
                    "access_programs": {"cyber": "standard"},
                    "usage": {"input_tokens": 11, "input_tokens_details": {"cached_tokens": 3},
                              "output_tokens": 2, "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 13}}
        events = [
            {"type": "response.created", "response": {**response, "status": "in_progress", "output": [], "usage": None}},
            {"type": "response.output_item.added", "output_index": 0, "item": {**message, "status": "in_progress", "content": []}},
            {"type": "response.content_part.added", "output_index": 0, "item_id": message["id"], "content_index": 0,
             "part": {"type": "output_text", "text": "", "annotations": []}},
            {"type": "response.output_text.delta", "output_index": 0, "item_id": message["id"], "content_index": 0, "delta": "OK."},
            {"type": "response.output_text.done", "output_index": 0, "item_id": message["id"], "content_index": 0, "text": "OK."},
            {"type": "response.content_part.done", "output_index": 0, "item_id": message["id"], "content_index": 0, "part": message["content"][0]},
            {"type": "response.output_item.done", "output_index": 0, "item": message},
            {"type": "response.completed", "response": response},
        ]
        payload = "".join(f"event: {event['type']}\ndata: {json.dumps({**event, 'sequence_number': index})}\n\n"
                          for index, event in enumerate(events)).encode()
        first_payload = payload
        if retry:
            first_payload = Path(os.environ["AXONHUB_CODEX_RETRY_FIXTURE"]).read_bytes()
        elif partial:
            partial_events = json.loads(json.dumps(events))
            for event in partial_events:
                if event.get("item", {}).get("type") == "message":
                    event["item"]["phase"] = "partial_answer"
                if event["type"] == "response.completed":
                    event["response"]["end_turn"] = False
                    event["response"]["output"][0]["phase"] = "partial_answer"
            first_payload = "".join(f"event: {event['type']}\ndata: {json.dumps({**event, 'sequence_number': index})}\n\n"
                                    for index, event in enumerate(partial_events)).encode()

        async def respond(reader, writer):
            try:
                headers = await asyncio.wait_for(reader.readuntil(b"\r\n\r\n"), 15)
                lines = headers.decode().split("\r\n")
                parsed = dict(line.lower().split(": ", 1) for line in lines[1:] if ": " in line)
                body = await reader.readexactly(int(parsed.get("content-length", "0")))
                if lines[0].startswith("POST /v1/responses "):
                    requests.append((parsed, json.loads(body)))
                    arrivals.append(asyncio.get_running_loop().time())
                    current_payload = first_payload if len(requests) == 1 else payload
                    writer.write(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\nContent-Length: "
                                 + str(len(current_payload)).encode() + b"\r\n\r\n" + current_payload)
                else:
                    writer.write(b"HTTP/1.1 404 Not Found\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
                await writer.drain()
            except (ConnectionError, asyncio.IncompleteReadError):
                pass
            except Exception as error:
                errors.append(str(error))
            finally:
                writer.close()
                await writer.wait_closed()

        with tempfile.TemporaryDirectory() as tmp:
            server = await asyncio.start_server(respond, "127.0.0.1", 0)
            try:
                with OwnedState(Path(tmp) / "owned") as state:
                    args = args_for(state.root, os.environ["PROBE_TEST_CODEX"])
                    args.url = f"http://127.0.0.1:{server.sockets[0].getsockname()[1]}/v1"
                    args.model = model
                    args.timeout = 90
                    runner = Runner(args, state, "synthetic-test-key")
                    await runner.check_codex()
                    from probe_common import config_text
                    def retry_config(*values):
                        return config_text(*values).replace("stream_max_retries = 0", "stream_max_retries = 1")
                    with mock.patch("probe_common.config_text", side_effect=retry_config if retry else config_text):
                        outcome = await runner.run({"id": "completed", "prompt": "Reply OK."}, "low")
                    self.assertEqual(outcome.status, "success", outcome.error)
                    self.assertEqual(outcome.returncode, 0)
                    self.assertTrue(outcome.thread_id)
                    multiplier = 2 if partial else 1
                    self.assertEqual(outcome.usage.get("input_tokens"), 11 * multiplier)
                    self.assertEqual(outcome.usage.get("cached_input_tokens"), 3 * multiplier)
                    self.assertEqual(outcome.usage.get("output_tokens"), 2 * multiplier)
                    self.assertEqual(len(requests), 2 if retry or partial else 1)
                    if retry:
                        self.assertGreaterEqual(arrivals[1] - arrivals[0], 2.7)
                    if partial:
                        history = requests[1][1]["input"]
                        self.assertTrue(any(item.get("phase") == "partial_answer" for item in history))
                    if os.environ.get("PROBE_TEST_CODEX_VERSION") == "0.162.0":
                        self.assertNotIn("instructions", requests[0][1])
                        self.assertTrue(any(item.get("role") == "developer" for item in requests[0][1]["input"]))
                    self.assertEqual(requests[0][0]["authorization"].lower(), "bearer synthetic-test-key")
                    self.assertEqual(requests[0][1]["model"], model)
                    self.assertNotIn("access_programs", requests[0][1])
                    self.assertEqual(errors, [])
                    self.assertEqual(runner.active_attempts, 0)
                    self.assertFalse(list(state.root.glob("attempt-*")))
            finally:
                server.close()
                await server.wait_closed()

    @unittest.skipUnless(os.environ.get("PROBE_TEST_CODEX_VERSION") == "0.162.0", "requires current agent-message protocol")
    async def test_gateway_agent_envelope_reaches_child_intact(self):
        # No model or external service is called. The real CLI executes one
        # synthetic spawn in its isolated scratch directory against this mock.
        sealed = "axonhub-agent-v1.synthetic-client-transport-fixture"
        child_seen = asyncio.Event()
        requests, errors = [], []
        model = os.environ.get("PROBE_TEST_MODEL", "gpt-6-sol")

        async def respond(reader, writer):
            try:
                headers = await asyncio.wait_for(reader.readuntil(b"\r\n\r\n"), 15)
                lines = headers.decode().split("\r\n")
                parsed = dict(line.lower().split(": ", 1) for line in lines[1:] if ": " in line)
                raw = await reader.readexactly(int(parsed.get("content-length", "0")))
                if not lines[0].startswith("POST /v1/responses "):
                    writer.write(b"HTTP/1.1 404 Not Found\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
                    await writer.drain()
                    return
                body = json.loads(raw)
                requests.append(body)
                number = len(requests)
                history = body.get("input", [])
                delivered = any(
                    item.get("type") == "agent_message" and any(
                        part.get("type") == "encrypted_content" and part.get("encrypted_content") == sealed
                        for part in item.get("content", [])
                    ) for item in history if isinstance(item, dict)
                )
                if delivered:
                    child_seen.set()
                if number == 1:
                    item = {"id": "fc_agent_fixture", "type": "function_call", "status": "completed",
                            "call_id": "call_agent_fixture", "namespace": "agents", "name": "spawn_agent",
                            "encrypted_function_args": ["message"],
                            "arguments": json.dumps({"task_name": "transport_fixture", "fork_turns": "none", "message": sealed})}
                else:
                    if not delivered:
                        await asyncio.wait_for(child_seen.wait(), 20)
                    item = {"id": f"msg_agent_{number}", "type": "message", "role": "assistant", "phase": "final_answer",
                            "status": "completed", "content": [{"type": "output_text", "text": "OK.", "annotations": []}]}
                response = {"id": f"resp_agent_{number}", "object": "response", "created_at": 1700000000,
                            "model": model, "status": "completed", "output": [item],
                            "usage": {"input_tokens": 11, "output_tokens": 2, "total_tokens": 13}}
                events = [{"type": "response.created", "response": {**response, "status": "in_progress", "output": []}},
                          {"type": "response.output_item.done", "output_index": 0, "item": item},
                          {"type": "response.completed", "response": response}]
                payload = "".join(f"event: {event['type']}\ndata: {json.dumps(event)}\n\n" for event in events).encode()
                writer.write(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\nContent-Length: "
                             + str(len(payload)).encode() + b"\r\n\r\n" + payload)
                await writer.drain()
            except (ConnectionError, asyncio.IncompleteReadError):
                pass
            except Exception as error:
                errors.append(str(error))
            finally:
                writer.close()
                await writer.wait_closed()

        with tempfile.TemporaryDirectory() as tmp:
            server = await asyncio.start_server(respond, "127.0.0.1", 0)
            try:
                with OwnedState(Path(tmp) / "owned") as state:
                    args = args_for(state.root, os.environ["PROBE_TEST_CODEX"])
                    args.url = f"http://127.0.0.1:{server.sockets[0].getsockname()[1]}/v1"
                    args.model, args.timeout = model, 60
                    runner = Runner(args, state, "synthetic-test-key")
                    from probe_common import config_text
                    def agent_config(*values):
                        return config_text(*values).replace(
                            "multi_agent_v2 = false",
                            'multi_agent_v2 = { enabled = true, tool_namespace = "agents", message_board_in_memory = true }',
                        )
                    with mock.patch("probe_common.config_text", side_effect=agent_config):
                        outcome = await runner.run({"id": "agent-transport", "prompt": "Run the synthetic transport fixture."}, "low")
                    self.assertTrue(child_seen.is_set(), f"encrypted delivery missing; requests={len(requests)}; {outcome.error}")
                    self.assertEqual(outcome.status, "success", outcome.error)
                    self.assertEqual(errors, [])
                    self.assertEqual(runner.active_attempts, 0)
            finally:
                server.close()
                await server.wait_closed()

    async def test_five_requests_overlap_and_cancel_cleanly(self):
        active = 0
        maximum = 0
        requests = []
        all_arrived = asyncio.Event()
        release = asyncio.Event()
        errors = []

        async def respond(reader, writer):
            nonlocal active, maximum
            try:
                headers = await asyncio.wait_for(reader.readuntil(b"\r\n\r\n"), 15)
                lines = headers.decode().split("\r\n")
                parsed = dict(line.lower().split(": ", 1) for line in lines[1:] if ": " in line)
                body = await reader.readexactly(int(parsed.get("content-length", "0")))
                if not lines[0].startswith("POST /v1/responses "):
                    writer.write(b"HTTP/1.1 404 Not Found\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
                    await writer.drain()
                    return
                requests.append((parsed, json.loads(body)))
                active += 1
                maximum = max(maximum, active)
                if active == 5:
                    all_arrived.set()
                await release.wait()
                active -= 1
                payload = json.dumps({"error": {"message": "synthetic fixture failure", "type": "server_error"}}).encode()
                writer.write(b"HTTP/1.1 500 Internal Server Error\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: " + str(len(payload)).encode() + b"\r\n\r\n" + payload)
                await writer.drain()
            except (ConnectionError, asyncio.IncompleteReadError):
                pass
            except Exception as error:
                errors.append(str(error))
            finally:
                writer.close()
                await writer.wait_closed()

        with tempfile.TemporaryDirectory() as tmp:
            server = await asyncio.start_server(respond, "127.0.0.1", 0)
            tasks = []
            try:
                with OwnedState(Path(tmp) / "owned") as state:
                    args = args_for(state.root, os.environ["PROBE_TEST_CODEX"])
                    args.url = f"http://127.0.0.1:{server.sockets[0].getsockname()[1]}/v1"
                    args.model = os.environ.get("PROBE_TEST_MODEL", "gpt-6-sol")
                    args.timeout = 90
                    runner = Runner(args, state, "synthetic-test-key")
                    await runner.check_codex()
                    tasks = [asyncio.create_task(runner.run({"id": f"q-{i}", "prompt": "Reply OK."}, "low")) for i in range(5)]
                    try:
                        await asyncio.wait_for(all_arrived.wait(), 60)
                        self.assertEqual(maximum, 5)
                        self.assertEqual(runner.active_attempts, 5)
                        self.assertEqual(len(list(state.root.glob("attempt-*"))), 5)
                        self.assertEqual(errors, [])
                        for headers, body in requests:
                            self.assertEqual(headers["authorization"].lower(), "bearer synthetic-test-key")
                            self.assertEqual(body["model"], args.model)
                    finally:
                        for task in tasks:
                            task.cancel()
                        await asyncio.gather(*tasks, return_exceptions=True)
                        release.set()
                    self.assertEqual(runner.active_attempts, 0)
                    self.assertFalse(list(state.root.glob("attempt-*")))
            finally:
                release.set()
                server.close()
                await server.wait_closed()
