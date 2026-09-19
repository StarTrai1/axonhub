#!/usr/bin/env python3
"""Wait for a selected time, make ONE new Codex attempt, clean it, then exit."""

import argparse
import asyncio
from datetime import datetime, timedelta, timezone
import json
from zoneinfo import ZoneInfo

from probe_common import (
    OwnedState, Questions, Runner, add_common_arguments, atomic_json, emit,
    entrypoint, load_bank, pause, positive, validate_common,
)
from quota_schedule import watch_windows


def wall_time(value):
    try:
        return datetime.strptime(value, "%H:%M:%S").time()
    except ValueError:
        return datetime.strptime(value, "%H:%M").time()


def choose_slot(args, now):
    if args.now:
        return now, "now:" + now.isoformat()
    if args.at:
        instant = datetime.fromisoformat(args.at.replace("Z", "+00:00"))
        if instant.tzinfo is None:
            raise ValueError("--at must include UTC offset, e.g. 2026-09-21T08:00:00+08:00")
        return instant, "at:" + instant.astimezone(timezone.utc).isoformat()
    zone = ZoneInfo(args.timezone)
    local = now.astimezone(zone)
    if args.daily:
        target_time = wall_time(args.daily)
        day = local.date()
        slot = datetime.combine(day, target_time, zone)
        if slot < local:
            slot += timedelta(days=1)
        label = "daily:"
    else:
        day_name, clock = args.weekly.split("@", 1)
        weekdays = {name: i for i, name in enumerate(("mon", "tue", "wed", "thu", "fri", "sat", "sun"))}
        if day_name.lower() not in weekdays:
            raise ValueError("--weekly must look like Mon@08:00:00")
        day = local.date() + timedelta(days=(weekdays[day_name.lower()] - local.weekday()) % 7)
        slot = datetime.combine(day, wall_time(clock), zone)
        if slot < local:
            slot += timedelta(days=7)
        label = "weekly:"
    # Reject nonexistent DST wall times rather than silently shift a request.
    if slot.astimezone(timezone.utc).astimezone(zone).replace(tzinfo=None) != slot.replace(tzinfo=None):
        raise ValueError("selected time does not exist due to DST; use --at with an explicit offset")
    return slot, label + slot.astimezone(timezone.utc).isoformat()


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_common_arguments(parser, "scheduled")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--now", action="store_true", help="one attempt immediately; suitable for systemd timers")
    mode.add_argument("--at", help="one date/time including offset; a past due slot runs once now")
    mode.add_argument("--daily", help="next HH:MM[:SS] in --timezone; exit after one attempt")
    mode.add_argument("--weekly", help="next Mon@HH:MM[:SS] in --timezone; exit after one attempt")
    mode.add_argument("--watch-windows", action="store_true", help="watch weekly reset plus two daily slots; remains running")
    parser.add_argument("--first-time", default="08:00:00", help="daily first request; second is five elapsed hours later")
    parser.add_argument("--quota-poll", type=positive, default=30.0, help="seconds between read-only snapshot refreshes")
    parser.add_argument("--quota-max-age", type=positive, default=3600.0)
    parser.add_argument("--catch-up-grace", type=positive, default=900.0, help="skip missed slots older than this many seconds")
    parser.add_argument("--timezone", default="Asia/Shanghai")
    args = parser.parse_args()
    key = validate_common(args)
    questions = Questions(load_bank(args.simple_bank))
    slot, slot_id = (None, None) if args.watch_windows else choose_slot(args, datetime.now(timezone.utc))
    specification = {"at": args.at, "daily": args.daily, "weekly": args.weekly, "timezone": args.timezone}
    with OwnedState(args.state_dir) as state:
        await state.recover()
        if args.cleanup_only:
            return 0
        runner = Runner(args, state, key)
        await runner.check_codex()
        if args.check:
            wall_time(args.first_time)
            ZoneInfo(args.timezone)
            emit("config_valid", mode="scheduled", scheduled_for=slot.isoformat() if slot else None, sends_requests=False)
            return 0
        if args.watch_windows:
            return await watch_windows(args, state, runner, questions, wall_time(args.first_time))
        claim_file = state.root / "last-slot.json"
        if claim_file.exists():
            claim = json.loads(claim_file.read_text())
            if claim.get("binding") != runner.binding():
                raise ValueError("state belongs to a different URL/key/model; use a separate --state-dir")
            if not args.now and claim.get("specification") == specification and claim.get("outcome") == "waiting":
                slot = datetime.fromisoformat(claim["scheduled_for"])
                slot_id = claim["slot_id"]
            if claim.get("slot_id") == slot_id:
                if claim.get("outcome") != "waiting":
                    emit("already_claimed", slot=slot_id, outcome=claim.get("outcome"), sends_requests=False)
                    return 0 if claim.get("outcome") == "success" else 1
        claim = {"slot_id": slot_id, "scheduled_for": slot.isoformat(), "outcome": "waiting",
                 "binding": runner.binding(), "specification": specification}
        atomic_json(claim_file, claim)
        emit("waiting", scheduled_for=slot.isoformat())
        while (remaining := (slot - datetime.now(timezone.utc)).total_seconds()) > 0:
            await pause(asyncio.Event(), min(remaining, 30), min(remaining, 30))
        claim["outcome"] = "unconfirmed"
        # Durable claim before contacting upstream: a crash must not replay a slot.
        atomic_json(claim_file, claim)
        result = await runner.run(questions.draw(), args.effort)
        claim["outcome"] = result.status
        atomic_json(claim_file, claim)
        emit("scheduled_attempt_finished", status=result.status, retry=False)
        return 0 if result.status == "success" else 1


if __name__ == "__main__":
    entrypoint(main)
