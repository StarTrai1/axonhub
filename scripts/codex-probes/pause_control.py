"""Pause at a cleanup boundary, without suspending cleanup itself."""

import asyncio
import os
import signal
import sys
import termios
import tty

from probe_common import emit


class PauseControl:
    def __init__(self):
        self.requested = False
        self.changed = asyncio.Event()
        self.active = None
        self.owner = None
        self.saved_terminal = None
        self.interrupting = False

    def request_pause(self):
        if self.requested:
            return
        self.requested = True
        self.changed.set()
        emit('pausing', reason='cancel requests, then finish owned cleanup')
        if self.active and not self.active.done() and not self.interrupting:
            self.interrupting = True
            self.active.cancel()

    def request_resume(self):
        self.requested = False
        self.changed.set()
        # If cleanup is still running, run() waits for it before returning.

    def read_key(self):
        key = os.read(sys.stdin.fileno(), 32).decode(errors='ignore').lower()
        for char in key:
            if char == 'p' or (char == ' ' and not self.requested):
                self.request_pause()
            elif char == 'r' or (char == ' ' and self.requested):
                self.request_resume()
            elif char == 'q':
                os.kill(os.getpid(), signal.SIGTERM)

    def __enter__(self):
        self.owner = asyncio.current_task()
        loop = asyncio.get_running_loop()
        loop.add_signal_handler(signal.SIGUSR1, self.request_pause)
        loop.add_signal_handler(signal.SIGUSR2, self.request_resume)
        if sys.stdin.isatty():
            self.saved_terminal = termios.tcgetattr(sys.stdin.fileno())
            tty.setcbreak(sys.stdin.fileno())
            loop.add_reader(sys.stdin.fileno(), self.read_key)
        emit('controls', keyboard='P pause / R resume / Space toggle / Q quit',
             signals='SIGUSR1 pause / SIGUSR2 resume', pid=os.getpid())
        return self

    def __exit__(self, *_):
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGUSR1, signal.SIGUSR2):
            loop.remove_signal_handler(sig)
        if self.saved_terminal:
            loop.remove_reader(sys.stdin.fileno())
            termios.tcsetattr(sys.stdin.fileno(), termios.TCSADRAIN, self.saved_terminal)

    async def checkpoint(self):
        if self.requested:
            emit('paused', cleanup_complete=True)
            while self.requested:
                self.changed.clear()
                await self.changed.wait()
            emit('resumed')

    async def run(self, factory):
        await self.checkpoint()
        self.interrupting = False
        self.active = asyncio.create_task(factory())
        try:
            return await self.active
        except asyncio.CancelledError:
            # Parent cancellation means shutdown. A pause only cancels the child.
            if self.owner.cancelling():
                raise
            return None
        finally:
            # Child cancellation propagates through Runner/acquire's cleanup.
            # Awaiting it above is the barrier before reporting paused.
            self.active = None


async def keepalive_cycle(control, progress, progress_file, runner, simple, reasoning, args):
    from keepalive import acquire
    from probe_common import atomic_json, pause

    while not runner.budget_exhausted():
        await control.checkpoint()
        emit('phase', phase=progress['phase'], high_demand=progress['high_demand'],
             concurrency=args.concurrency if progress['phase'] == 1 else 1)
        if progress['phase'] == 1:
            status = await control.run(lambda: acquire(runner, simple, args))
            if status is None:
                continue
            if status != 'success':
                emit('stopped', reason=status)
                return 0 if status == 'budget' else 1
            progress.update(phase=2, high_demand=0)
            atomic_json(progress_file, progress)
        else:
            async def attempt():
                await pause(asyncio.Event(), args.interval_min, args.interval_max)
                return await runner.run(reasoning.draw(), args.reasoning_effort)
            result = await control.run(attempt)
            if result is None:
                continue
            if result.status == 'high_demand':
                progress['high_demand'] += 1
                if progress['high_demand'] >= 10:
                    progress.update(phase=1, high_demand=0)
                atomic_json(progress_file, progress)
            elif result.status not in ('success', 'budget'):
                emit('stopped', reason=result.status)
                return 1
    emit('stopped', reason='budget', attempts=runner.attempts, reported_tokens=runner.reported_tokens)
    return 0
