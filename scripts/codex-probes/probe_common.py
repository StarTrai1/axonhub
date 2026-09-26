"""Linux-only, isolated Codex CLI runs. No gateway management/delete calls."""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import random
import re
import shutil
import signal
import time
from dataclasses import dataclass, field
from urllib.parse import quote, quote_plus, urlsplit
import uuid

HERE = Path(__file__).resolve().parent
HIGH_DEMAND = "We're currently experiencing high demand, which may cause temporary errors"
MODEL_CAPACITY = re.compile(r"当前模型\s+[^\s，,]{1,200}\s+负载已经达到上限[，,]\s*请稍后重试")
ERROR_LIMIT = 2048
JOB_NAME = re.compile(r"attempt-[0-9a-f]{32}\Z")
RNG = random.SystemRandom()


def emit(event: str, **fields: object) -> None:
    print(json.dumps({"at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                      "event": event, **fields}, ensure_ascii=False), flush=True)


def atomic_json(path: Path, data: object) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    with open(temporary, "w", encoding="utf-8") as stream:
        os.chmod(temporary, 0o600)
        json.dump(data, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def positive(value: str) -> float:
    result = float(value)
    if not math.isfinite(result) or result <= 0:
        raise argparse.ArgumentTypeError("must be a finite positive number")
    return result


def nonnegative(value: str) -> int:
    result = int(value)
    if result < 0:
        raise argparse.ArgumentTypeError("must be >= 0")
    return result


def add_common_arguments(parser: argparse.ArgumentParser, state_name: str) -> None:
    parser.add_argument("--url", required=True, help="AxonHub API base URL, normally ending in /v1")
    parser.add_argument("--key-env", default="AXONHUB_PROBE_API_KEY", help="environment variable holding a dedicated channel-restricted key")
    parser.add_argument("--model", required=True)
    parser.add_argument("--codex", help="Codex executable; default: standalone current, then PATH; pinned for this invocation")
    parser.add_argument("--state-dir", type=Path, default=Path.home() / ".local/state/axonhub-probes" / state_name)
    parser.add_argument("--timeout", type=positive, default=600.0, help="hard time limit per CLI process in seconds")
    parser.add_argument("--max-attempts", type=nonnegative, default=0, help="per invocation; 0 means unlimited")
    parser.add_argument("--max-tokens", type=nonnegative, default=0, help="stop after reported input+output tokens reach this budget; 0 disables")
    parser.add_argument("--simple-bank", type=Path, default=HERE / "questions-simple.json")
    parser.add_argument("--effort", default="low", help="phase-one/scheduled model reasoning effort")
    parser.add_argument("--supports-websockets", action="store_true", help="enable only if this custom provider supports native Codex WebSockets")
    parser.add_argument("--check", action="store_true", help="validate config and local Codex version; send no model requests")
    parser.add_argument("--cleanup-only", action="store_true", help="clean owned crash leftovers and exit without a request")


def resolve_codex(executable: str | None) -> str:
    if executable:
        candidate = shutil.which(str(Path(executable).expanduser()))
    else:
        standalone = Path.home() / ".codex/packages/standalone/current/codex"
        candidate = str(standalone) if standalone.is_file() and os.access(standalone, os.X_OK) else shutil.which("codex")
    if not candidate:
        raise ValueError("Codex executable not found; set --codex to the installed CLI")
    # Resolve current/symlinks once so an update cannot change binaries mid-run.
    return str(Path(candidate).resolve())


def validate_common(args: argparse.Namespace) -> str:
    if os.name != "posix" or not Path("/proc/self/stat").exists():
        raise ValueError("these scripts require Linux with /proc")
    url = urlsplit(args.url)
    if url.scheme not in ("http", "https") or not url.hostname or url.username or url.password or url.query or url.fragment:
        raise ValueError("--url must be an HTTP(S) API base URL without credentials, query, or fragment")
    if url.path.rstrip("/").endswith("/responses"):
        raise ValueError("use the API base URL (e.g. /v1), not /v1/responses")
    if not args.model.strip() or not args.effort.strip():
        raise ValueError("model and effort cannot be blank")
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", args.key_env):
        raise ValueError("invalid key environment variable name")
    key = os.environ.get(args.key_env, "")
    if not key and not args.cleanup_only:
        raise ValueError(f"set {args.key_env} before running; keys are never accepted on the command line")
    return key


def load_bank(path: Path) -> list[dict]:
    entries = json.loads(path.read_text(encoding="utf-8"))["questions"]
    if not entries or not isinstance(entries, list):
        raise ValueError("question bank must contain a nonempty questions array")
    ids = set()
    for entry in entries:
        if not isinstance(entry, dict):
            raise ValueError("question entries must be objects")
        if not isinstance(entry.get("id"), str) or entry["id"] in ids:
            raise ValueError("question IDs must be unique strings")
        ids.add(entry["id"])
        if not isinstance(entry.get("prompt"), str) or not entry["prompt"].strip() or len(entry["prompt"]) > 16000:
            raise ValueError("question prompts must be nonempty and <=16000 characters")
    return entries


class Questions:
    def __init__(self, entries: list[dict]):
        self.entries = entries
        self.remaining: list[dict] = []

    def draw(self) -> dict:
        if not self.remaining:
            self.remaining = RNG.sample(self.entries, len(self.entries))
        return self.remaining.pop()


def process_identity(pid: int) -> str | None:
    try:
        # comm may contain spaces/parentheses; starttime is field 22 after it.
        stat = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
        boot = Path("/proc/sys/kernel/random/boot_id").read_text().strip()
        return f"{boot}:{stat[19]}"
    except (FileNotFoundError, ProcessLookupError):
        return None


class OwnedState:
    """Exclusive private root; refuse unknown paths instead of broad deletion."""

    def __init__(self, path: Path):
        self.root = path.expanduser().absolute()
        self.lock = None
        self.owner = ""

    def __enter__(self) -> OwnedState:
        if self.root.is_symlink() or self.root.resolve() != self.root:
            raise ValueError("state directory and its ancestors must not be symlinks")
        self.root.mkdir(parents=True, mode=0o700, exist_ok=True)
        if self.root.stat().st_uid != os.getuid():
            raise ValueError("state directory must belong to this user")
        if not (self.root / ".owner.json").exists() and any(self.root.iterdir()):
            raise ValueError("refusing a nonempty unowned state directory")
        lock_path = self.root / ".lock"
        descriptor = os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        self.lock = os.fdopen(descriptor, "w")
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            marker = self.root / ".owner.json"
            if marker.exists():
                if marker.is_symlink():
                    raise ValueError("unsafe owner marker")
                data = json.loads(marker.read_text())
                if data.get("purpose") != "axonhub-codex-probes-v1":
                    raise ValueError("state directory has an unrelated owner marker")
                self.owner = data["owner"]
                uuid.UUID(self.owner)
            else:
                if any(p.name != ".lock" for p in self.root.iterdir()):
                    raise ValueError("refusing a nonempty unowned state directory")
                self.owner = str(uuid.uuid4())
                atomic_json(marker, {"owner": self.owner, "purpose": "axonhub-codex-probes-v1"})
            os.chmod(self.root, 0o700)
            return self
        except BaseException:
            self.lock.close()
            raise

    def __exit__(self, *_: object) -> None:
        if self.lock:
            self.lock.close()

    def create_job(self) -> tuple[Path, dict]:
        job = self.root / ("attempt-" + uuid.uuid4().hex)
        job.mkdir(mode=0o700)
        marker = {"owner": self.owner, "job": job.name, "pid": None, "identity": None}
        atomic_json(job / ".job.json", marker)
        (job / "codex-home").mkdir(mode=0o700)
        (job / "work").mkdir(mode=0o700)
        return job, marker

    def verify_job(self, job: Path) -> dict:
        if job.parent != self.root or not JOB_NAME.fullmatch(job.name) or job.is_symlink():
            raise ValueError("refusing to clean a path outside an owned attempt")
        marker_path = job / ".job.json"
        if marker_path.is_symlink():
            raise ValueError("unsafe job marker")
        marker = json.loads(marker_path.read_text())
        if marker.get("owner") != self.owner or marker.get("job") != job.name:
            raise ValueError("attempt owner mismatch; cleanup stopped")
        return marker

    def remove_job(self, job: Path) -> None:
        self.verify_job(job)
        shutil.rmtree(job)  # Python's fd-based Linux implementation resists symlink traversal.

    async def recover(self) -> None:
        for job in self.root.iterdir():
            if not JOB_NAME.fullmatch(job.name):
                continue
            marker = self.verify_job(job)
            pid = marker.get("pid")
            if pid and marker.get("identity") and marker["identity"] == process_identity(pid):
                if os.getpgid(pid) != pid:
                    raise ValueError("orphan process is no longer its recorded group leader")
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(pid, signal.SIGTERM)
                for _ in range(50):
                    if process_identity(pid) != marker["identity"]:
                        break
                    await asyncio.sleep(0.1)
                if process_identity(pid) == marker["identity"]:
                    with contextlib.suppress(ProcessLookupError):
                        os.killpg(pid, signal.SIGKILL)
                    await asyncio.sleep(0.2)
            elif not pid:
                # A crash between spawning and journaling must not cause deletion
                # under a still-running child. Fail closed if its private home is live.
                needle = f"CODEX_HOME={job / 'codex-home'}".encode()
                for proc in Path("/proc").iterdir():
                    if not proc.name.isdigit():
                        continue
                    try:
                        if needle in (proc / "environ").read_bytes().split(b"\0"):
                            raise ValueError(f"unclaimed live attempt {job.name}; stop its process before --cleanup-only")
                    except (PermissionError, FileNotFoundError, ProcessLookupError):
                        continue
            self.remove_job(job)
            emit("recovered_local_attempt", attempt=job.name)


@dataclass
class Outcome:
    status: str
    thread_id: str | None = None
    usage: dict = field(default_factory=dict)
    returncode: int | None = None
    error: str | None = None
    error_source: str | None = None
    http_status: int | None = None


def is_high_demand(message: str) -> bool:
    # Capacity is an explicit provider message, not every 500/429 or quota error.
    # Never retry a denied request even if it also quotes an older capacity error.
    if re.search(r"status(?: code)?\s*[:=]?\s*(?:401|403)\b|\b(?:unauthorized|forbidden|invalid[_ ]api[_ ]key|insufficient_quota|banned|suspended|account_deactivated)\b|封禁", message, re.I):
        return False
    normalized = " ".join(message.translate(str.maketrans("‘’ʼ＇", "''''")).split())
    return HIGH_DEMAND.casefold() in normalized.casefold() or MODEL_CAPACITY.search(normalized) is not None


def http_error_status(message: str) -> int | None:
    # Codex exec JSONL exposes the error message, not a separate HTTP status.
    # Read explicit transport status text; an arbitrary number is not evidence.
    match = re.search(r"\b(?:unexpected status(?: code)?|HTTP(?:/\d(?:\.\d)?)?(?: status(?: code)?)?)\s*[:=]?\s*([1-5][0-9]{2})\b", message, re.I)
    return int(match[1]) if match else None


def redact_error(message: str, key: str) -> str:
    # Redact before truncation: a key crossing the output boundary must not leak.
    if key:
        for secret in sorted({key, quote(key, safe=""), quote_plus(key), json.dumps(key)[1:-1]}, key=len, reverse=True):
            message = message.replace(secret, "[REDACTED]")
    message = re.sub(r"(?im)\b(?:authorization|proxy-authorization|cookie|set-cookie)\b[\"']?\s*[:=]\s*[^\r\n]+", "[REDACTED HEADER]", message)
    message = re.sub(
        r'''(?i)(\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|token|password|client[_-]?secret|secret|key)\b["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;&}\]]+)''',
        r"\1[REDACTED]", message)
    message = re.sub(r'''(?i)\bBearer\s+[^\s"',;}\]]+''', "Bearer [REDACTED]", message)
    message = re.sub(r"(https?://)[^\s/@]+:[^\s/@]+@", r"\1[REDACTED]@", message)
    # Keep JSON log entries single-line and strip terminal control sequences.
    message = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", message)
    message = " ".join(message.split())
    message = re.sub(r"[\x00-\x1f\x7f]", "", message)
    return message if len(message) <= ERROR_LIMIT else message[:ERROR_LIMIT - 3] + "..."


class StderrErrors:
    """Keep only the latest explicit CLI error line, never raw stderr history."""

    def __init__(self, key: str):
        self.key = key
        self.pending = b""
        self.dropping = False
        self.last_error = ""

    def feed(self, chunk: bytes) -> None:
        for part in chunk.splitlines(keepends=True):
            ended = part.endswith((b"\n", b"\r"))
            if not self.dropping:
                self.pending += part
                if len(self.pending) > 16384:
                    # Drop whole oversized lines, not a potentially secret suffix.
                    self.pending = b""
                    self.dropping = True
                elif ended:
                    self._line()
            if ended:
                self.pending = b""
                self.dropping = False

    def _line(self) -> None:
        line = self.pending.decode(errors="replace").strip()
        if re.match(r"(?i)^error\s*:", line):
            self.last_error = redact_error(line, self.key)

    def finish(self) -> None:
        if not self.dropping:
            self._line()
        self.pending = b""


class Events:
    def __init__(self, key: str = ""):
        self.key = key
        self.thread_id: str | None = None
        self.completed = False
        self.failed = False
        self.last_error = ""
        self.retryable = False
        self.error_source: str | None = None
        self.http_status: int | None = None
        self.usage: dict = {}

    def feed(self, line: bytes) -> None:
        try:
            event = json.loads(line)
        except (ValueError, UnicodeError):
            return
        if not isinstance(event, dict):
            return
        kind = event.get("type")
        if kind == "thread.started":
            self.thread_id = event.get("thread_id")
        elif kind == "turn.completed":
            self.completed = True
            usage = event.get("usage", {})
            if isinstance(usage, dict):
                self.usage = {k: v for k, v in usage.items() if isinstance(v, int) and not isinstance(v, bool) and v >= 0}
        elif kind == "turn.failed":
            self.failed = True
            error = event.get("error", {})
            self._error(error.get("message", "") if isinstance(error, dict) else "", kind)
        elif kind == "error" and not self.failed:
            self._error(event.get("message", ""), kind)

    def _error(self, message: object, source: str) -> None:
        message = message if isinstance(message, str) else ""
        self.retryable = is_high_demand(message)
        self.http_status = http_error_status(message)
        self.last_error = redact_error(message, self.key)
        self.error_source = source

    def outcome(self, returncode: int) -> Outcome:
        # Agent text can quote errors. Only structured error events count.
        if returncode == 0 and self.completed and not self.failed:
            status = "success"
        elif self.http_status in (401, 403):
            status = "error"
        elif self.retryable:
            status = "high_demand"
        elif self.http_status == 500:
            status = "http_500"
        else:
            status = "error"
        return Outcome(status, self.thread_id, self.usage, returncode,
                       self.last_error if status != "success" else None,
                       self.error_source if status != "success" else None,
                       self.http_status if status != "success" else None)


def config_text(args: argparse.Namespace, effort: str) -> str:
    # TOML strings use JSON-compatible escaping. Never include the secret key.
    q = lambda value: json.dumps(value, ensure_ascii=False)
    return f'''model = {q(args.model)}
model_provider = "axonhub_probe"
model_reasoning_effort = {q(effort)}
model_verbosity = "low"
approval_policy = "never"
sandbox_mode = "read-only"
web_search = "disabled"
[history]
persistence = "none"
[analytics]
enabled = false
[feedback]
enabled = false
[features]
shell_tool = false
unified_exec = false
apps = false
plugins = false
multi_agent = false
multi_agent_v2 = false
memories = false
memory_tool = false
image_generation = false
browser_use = false
computer_use = false
codex_hooks = false
hooks = false
unbounded_connection_retries = false
[model_providers.axonhub_probe]
name = "AxonHub probe"
base_url = {q(args.url.rstrip('/'))}
env_key = "AXONHUB_ISOLATED_PROBE_KEY"
wire_api = "responses"
requires_openai_auth = false
request_max_retries = 0
stream_max_retries = 0
supports_websockets = {str(args.supports_websockets).lower()}
stream_idle_timeout_ms = {int(args.timeout * 1000)}
'''


class Runner:
    def __init__(self, args: argparse.Namespace, state: OwnedState, key: str):
        self.args, self.state, self.key = args, state, key
        self.attempts = 0
        self.active_attempts = 0
        self.reported_tokens = 0

    def binding(self) -> str:
        # Changing target, key or model must not inherit another target's phase/slot.
        return hashlib.sha256(json.dumps([self.args.url, self.args.model, self.key]).encode()).hexdigest()

    def budget_exhausted(self) -> bool:
        return bool((self.args.max_attempts and self.attempts >= self.args.max_attempts)
                    or (self.args.max_tokens and self.reported_tokens >= self.args.max_tokens))

    async def check_codex(self) -> None:
        self.args.codex = resolve_codex(self.args.codex)
        proc = await asyncio.create_subprocess_exec(self.args.codex, "--version", stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
        try:
            out, _ = await asyncio.wait_for(proc.communicate(), 10)
        except BaseException:
            proc.kill()
            await proc.wait()
            raise
        version = out.decode(errors="replace").strip()
        parsed = re.search(r"codex(?:-cli)?\s+(\d+)\.(\d+)\.(\d+)", version, re.I)
        if proc.returncode != 0 or not parsed:
            raise ValueError("Codex --version failed; install/pin the official CLI")
        if tuple(int(part) for part in parsed.groups()) < (0, 155, 1):
            raise ValueError("Codex >= 0.155.1 required; supported baseline is 0.155.1, with compatibility verified through 0.157.1")
        emit("codex_version", version=version[:120], executable=self.args.codex)

    async def run(self, question: dict, effort: str) -> Outcome:
        if self.budget_exhausted():
            return Outcome("budget")
        self.attempts += 1
        attempt_number = self.attempts
        job, marker = self.state.create_job()
        proc = None
        active_registered = False
        readers = []
        events = Events(self.key)
        stderr_errors = StderrErrors(self.key)
        outcome = Outcome("error")
        try:
            (job / "codex-home/config.toml").write_text(config_text(self.args, effort), encoding="utf-8")
            # A dedicated child environment uses CODEX_HOME for its intended purpose;
            # the caller's HOME, CODEX_HOME, files, config and credentials are untouched.
            env = {k: v for k, v in os.environ.items() if not k.startswith(("CODEX_", "OPENAI_")) and k != self.args.key_env}
            env["CODEX_HOME"] = str(job / "codex-home")
            env["AXONHUB_ISOLATED_PROBE_KEY"] = self.key
            spawn = asyncio.create_task(asyncio.create_subprocess_exec(
                self.args.codex, "exec", "--json", "--ephemeral", "--skip-git-repo-check", "--color", "never", "-",
                cwd=job / "work", env=env, start_new_session=True,
                stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
                limit=2**20,
            ))
            cancelled = False
            try:
                proc = await asyncio.shield(spawn)
            except asyncio.CancelledError:
                # Creation is not cancellable atomically. Recover its process
                # handle so the finally block can reap it before deleting files.
                proc = await spawn
                cancelled = True
            marker.update(pid=proc.pid, identity=process_identity(proc.pid))
            atomic_json(job / ".job.json", marker)
            self.active_attempts += 1
            active_registered = True
            emit("attempt_started", attempt=job.name, attempt_number=attempt_number,
                 pid=proc.pid, active_attempts=self.active_attempts, question_id=question["id"])
            if cancelled:
                raise asyncio.CancelledError

            async def stdout() -> None:
                while line := await proc.stdout.readline():
                    events.feed(line)

            async def stderr() -> None:
                while chunk := await proc.stderr.read(8192):
                    stderr_errors.feed(chunk)
                stderr_errors.finish()

            readers = [asyncio.create_task(stdout()), asyncio.create_task(stderr())]
            prompt = question["prompt"] + "\n简短回答即可；不要调用工具、联网、读写文件或创建子代理。"
            try:
                proc.stdin.write(prompt.encode())
                await proc.stdin.drain()
            except (BrokenPipeError, ConnectionResetError):
                pass  # Early CLI failures still need their error streams collected.
            finally:
                proc.stdin.close()
            try:
                await asyncio.wait_for(asyncio.gather(proc.wait(), *readers), self.args.timeout)
                outcome = events.outcome(proc.returncode)
                if outcome.status != "success" and not outcome.error:
                    outcome.error = stderr_errors.last_error or f"Codex exited with code {proc.returncode} without a successful turn or an error message"
                    outcome.error_source = "stderr" if stderr_errors.last_error else "process"
            except asyncio.TimeoutError:
                outcome = Outcome("timeout", events.thread_id, error=f"Codex exceeded the {self.args.timeout:g}s attempt timeout", error_source="process")
            self.reported_tokens += outcome.usage.get("input_tokens", 0) + outcome.usage.get("output_tokens", 0)
        finally:
            async def cleanup() -> None:
                if proc is not None:
                    # Stop the entire private process group, then reap before deletion.
                    with contextlib.suppress(ProcessLookupError):
                        os.killpg(proc.pid, signal.SIGINT)
                    try:
                        await asyncio.wait_for(proc.wait(), 10)
                    except asyncio.TimeoutError:
                        with contextlib.suppress(ProcessLookupError):
                            os.killpg(proc.pid, signal.SIGKILL)
                        await proc.wait()
                    # Also stop any helper left after the main process exited.
                    with contextlib.suppress(ProcessLookupError):
                        os.killpg(proc.pid, signal.SIGKILL)
                for task in readers:
                    if not task.done():
                        task.cancel()
                if readers:
                    await asyncio.gather(*readers, return_exceptions=True)
                self.state.remove_job(job)
                if active_registered:
                    self.active_attempts -= 1
                emit("local_attempt_cleaned", attempt=job.name, active_attempts=self.active_attempts)

            cleanup_task = asyncio.create_task(cleanup())
            try:
                await asyncio.shield(cleanup_task)
            except asyncio.CancelledError:
                await cleanup_task
                raise
        emit("attempt_result", status=outcome.status, question_id=question["id"], thread_id=outcome.thread_id,
             attempts=self.attempts, attempt_number=attempt_number, attempt=job.name,
             usage=outcome.usage, reported_tokens=self.reported_tokens,
             returncode=outcome.returncode, error=outcome.error, error_source=outcome.error_source,
             http_status=outcome.http_status)
        return outcome


async def pause(stop: asyncio.Event, minimum: float, maximum: float) -> None:
    delay = RNG.uniform(minimum, maximum)
    try:
        await asyncio.wait_for(stop.wait(), timeout=delay)
    except asyncio.TimeoutError:
        pass


async def with_signals(main) -> int:
    task = asyncio.create_task(main())
    loop = asyncio.get_running_loop()
    interrupted = False

    def stop() -> None:
        nonlocal interrupted
        if not interrupted:
            interrupted = True
            emit("stopping", reason="signal; cleaning owned attempts")
            task.cancel()

    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stop)
    try:
        return await task
    except asyncio.CancelledError:
        return 130
    finally:
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.remove_signal_handler(sig)


def entrypoint(main) -> None:
    try:
        raise SystemExit(asyncio.run(with_signals(main)))
    except (ValueError, OSError, KeyError) as error:
        # Errors here contain only local paths/config, never raw provider output.
        emit("stopped", reason=str(error))
        raise SystemExit(2) from None
