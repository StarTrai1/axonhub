"""Opt-in hosted check: real packaged Codex, loopback mock only, synthetic key."""

import asyncio
import json
import os
from pathlib import Path
import tempfile
import unittest

from probe_common import OwnedState, Runner
from test_probes import args_for


@unittest.skipUnless(os.environ.get("PROBE_TEST_CODEX"), "real CLI fixture runs only in hosted CI")
class CLIIntegrationTest(unittest.IsolatedAsyncioTestCase):
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
