#!/usr/bin/env python3
"""Two-phase Codex probe loop; each attempt is a new disposable local session."""

import argparse
import asyncio
import json
from pathlib import Path

from probe_common import (
    HERE, OwnedState, Questions, Runner, add_common_arguments, atomic_json,
    emit, entrypoint, load_bank, pause, positive, validate_common,
)


async def acquire(runner, questions, args):
    stop = asyncio.Event()
    winner = asyncio.get_running_loop().create_future()

    async def worker(index):
        try:
            await pause(stop, index * args.stagger, (index + 1) * args.stagger)
            failures = 0
            while not stop.is_set():
                if runner.budget_exhausted():
                    return
                result = await runner.run(questions.draw(), args.effort)
                if result.status not in ("high_demand", "http_500"):
                    if not winner.done():
                        winner.set_result(result.status)
                    stop.set()
                    return
                if runner.budget_exhausted():
                    return
                failures += 1
                # Jitter spreads load; it does not impersonate people or bypass bans.
                floor = min(args.retry_max, args.retry_min * 2 ** min(failures - 1, 12))
                await pause(stop, floor, min(args.retry_max, floor * 1.5))
        except Exception as error:
            if not winner.done():
                winner.set_exception(error)
            raise

    tasks = [asyncio.create_task(worker(index)) for index in range(args.concurrency)]
    def worker_finished(_):
        if all(task.done() for task in tasks) and not winner.done():
            winner.set_result("budget")
    for task in tasks:
        task.add_done_callback(worker_finished)
    try:
        return await winner
    finally:
        stop.set()
        for task in tasks:
            if not task.done():
                task.cancel()
        results = await asyncio.gather(*tasks, return_exceptions=True)
        for result in results:
            if isinstance(result, Exception):
                raise result  # Never enter phase two after a failed cleanup.


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_common_arguments(parser, "keepalive")
    parser.add_argument("--restart-phase-one", action="store_true", help="reset saved phase after owned cleanup; does not affect other sessions")
    parser.add_argument("--concurrency", type=int, default=2)
    parser.add_argument("--stagger", type=positive, default=0.5, help="stagger phase-one starts, seconds; worker N starts within [(N-1)*stagger, N*stagger]")
    parser.add_argument("--retry-min", type=positive, default=10.0)
    parser.add_argument("--retry-max", type=positive, default=120.0)
    parser.add_argument("--interval-min", type=positive, default=30.0, help="phase-two delay between attempts")
    parser.add_argument("--interval-max", type=positive, default=60.0)
    parser.add_argument("--reasoning-effort", default="high")
    parser.add_argument("--reasoning-bank", type=Path, default=HERE / "questions-reasoning.json")
    args = parser.parse_args()
    key = validate_common(args)
    if not 1 <= args.concurrency <= 32:
        raise ValueError("--concurrency must be between 1 and 32")
    if args.retry_min > args.retry_max or args.interval_min > args.interval_max:
        raise ValueError("minimum delay must not exceed maximum delay")
    simple, reasoning = Questions(load_bank(args.simple_bank)), Questions(load_bank(args.reasoning_bank))
    with OwnedState(args.state_dir) as state:
        await state.recover()
        if args.cleanup_only:
            return 0
        runner = Runner(args, state, key)
        await runner.check_codex()
        if args.check:
            emit("config_valid", mode="keepalive", concurrency=args.concurrency, sends_requests=False)
            return 0
        progress_file = state.root / "progress.json"
        progress = {"phase": 1, "high_demand": 0, "binding": runner.binding()}
        if progress_file.exists():
            progress = json.loads(progress_file.read_text())
            if progress.get("binding") != runner.binding():
                raise ValueError("state belongs to a different URL/key/model; use a separate --state-dir")
            if progress.get("phase") not in (1, 2) or not isinstance(progress.get("high_demand"), int) or not 0 <= progress["high_demand"] < 10:
                raise ValueError("invalid keepalive progress; inspect progress.json")
        if args.restart_phase_one:
            progress.update(phase=1, high_demand=0)
            atomic_json(progress_file, progress)
        emit("phase_loaded", phase=progress["phase"], resumed=progress_file.exists(),
             restarted=args.restart_phase_one, concurrency=args.concurrency if progress["phase"] == 1 else 1)
        from pause_control import PauseControl, keepalive_cycle
        with PauseControl() as control:
            return await keepalive_cycle(control, progress, progress_file, runner, simple, reasoning, args)



if __name__ == "__main__":
    entrypoint(main)
