"""Read-only AxonHub quota snapshots plus two daily local schedule slots."""

import asyncio
from datetime import datetime, timedelta, timezone
import json
import time
from urllib.error import HTTPError, URLError
from urllib.request import HTTPRedirectHandler, Request, build_opener
from zoneinfo import ZoneInfo

from probe_common import atomic_json, emit, pause


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None  # Never forward API keys to a redirect target.


def fetch_windows(base_url, key):
    request = Request(base_url.rstrip('/') + '/axonhub/quota-windows',
                      headers={'Authorization': 'Bearer ' + key, 'Accept': 'application/json'})
    try:
        with build_opener(NoRedirect).open(request, timeout=15) as response:
            body = response.read(262145)
            if len(body) > 262144:
                raise ValueError('quota response too large')
            result = json.loads(body)
    except HTTPError as error:
        # Do not print raw provider/server response bodies or request headers.
        raise ValueError(f'quota snapshot HTTP {error.code}; check endpoint, single-channel key profile and quota collection') from None
    except URLError:
        raise ConnectionError('quota snapshot network error') from None
    if not isinstance(result, dict) or not isinstance(result.get('windows'), list):
        raise ValueError('invalid quota snapshot')
    return result


def instant(value):
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None:
        raise ValueError('quota timestamps must include a UTC offset')
    return parsed.astimezone(timezone.utc)


def weekly_reset(snapshot, now, max_age):
    observed = instant(snapshot['observed_at'])
    server = instant(snapshot['server_time'])
    if abs((server - now).total_seconds()) > 60:
        raise ValueError('server/client clock skew exceeds 60 seconds; synchronize clocks')
    if (now - observed).total_seconds() > max_age:
        return None
    choices = set()
    for window in snapshot['windows']:
        if window.get('window') in ('7d', 'weekly') and window.get('next_reset_at'):
            choices.add(instant(window['next_reset_at']))
    if len(choices) > 1:
        raise ValueError('multiple weekly windows disagree; no unambiguous weekly reset')
    return next(iter(choices), None)


def daily_slots(now, first_time, zone):
    local_day = now.astimezone(zone).date()
    slots = []
    for offset in (-1, 0, 1):
        first = datetime.combine(local_day + timedelta(days=offset), first_time, zone)
        # The second request is five elapsed hours later, including DST changes.
        first_utc = first.astimezone(timezone.utc)
        if first_utc.astimezone(zone).replace(tzinfo=None) != first.replace(tzinfo=None):
            continue  # Nonexistent DST local time: no guessed trigger.
        for index, slot in enumerate((first_utc, first_utc + timedelta(hours=5)), 1):
            slots.append((f'daily:{first.date()}:{index}', slot))
    return slots


async def watch_windows(args, state, runner, questions, first_time):
    zone = ZoneInfo(args.timezone)
    path = state.root / 'window-schedule.json'
    specification = {'first_time': first_time.isoformat(), 'timezone': args.timezone}
    journal = {'binding': runner.binding(), 'specification': specification, 'slots': {}, 'weekly_reset': None}
    if path.exists():
        journal = json.loads(path.read_text())
        if journal.get('binding') != runner.binding() or journal.get('specification') != specification:
            raise ValueError('window schedule belongs to another target/time; use another state-dir')
    next_poll = 0.0
    while not runner.budget_exhausted():
        now = datetime.now(timezone.utc)
        slots = journal['slots']
        for slot_id, slot in daily_slots(now, first_time, zone):
            if (now - slot).total_seconds() <= args.catch_up_grace and slot_id not in slots:
                slots[slot_id] = {'at': slot.isoformat(), 'outcome': 'waiting'}
        waiting_delays = [(instant(value['at']) - now).total_seconds() for value in slots.values()
                          if value['outcome'] == 'waiting']
        # Do not block an already scheduled imminent slot on a network read.
        if time.monotonic() >= next_poll and (next_poll == 0 or not waiting_delays or min(waiting_delays) > 16):
            try:
                snapshot = await asyncio.to_thread(fetch_windows, args.url, runner.key)
                reset = weekly_reset(snapshot, datetime.now(timezone.utc), args.quota_max_age)
            except ConnectionError:
                emit('quota_poll_unavailable')
            else:
                if reset:
                    slot_id = 'weekly:' + reset.isoformat()
                    old = journal.get('weekly_reset')
                    if old and old != slot_id and old in slots and slots[old]['outcome'] == 'waiting':
                        slots[old]['outcome'] = 'superseded'
                    journal['weekly_reset'] = slot_id
                    if slot_id not in slots:
                        slots[slot_id] = {'at': reset.isoformat(), 'outcome': 'waiting'}
                        emit('weekly_reset_scheduled', at=reset.isoformat(), observed_at=snapshot['observed_at'])
                else:
                    emit('weekly_snapshot_unavailable', reason='missing or stale weekly window; daily slots remain enabled')
            next_poll = time.monotonic() + args.quota_poll
        # Coalesce coincident weekly/daily slots, and skip old missed slots rather
        # than burst all old tasks after an outage. Claimed attempts never replay.
        due = [(key, value) for key, value in slots.items()
               if value['outcome'] == 'waiting' and instant(value['at']) <= now]
        due.sort(key=lambda pair: instant(pair[1]['at']))
        if due:
            latest = instant(due[-1][1]['at'])
            chosen = []
            for slot_id, value in due:
                at = instant(value['at'])
                if (now - at).total_seconds() > args.catch_up_grace or (latest - at).total_seconds() > 1:
                    value['outcome'] = 'missed'
                    emit('missed_slot', slot=slot_id)
                else:
                    value['outcome'] = 'unconfirmed'
                    chosen.append(slot_id)
            atomic_json(path, journal)
            if chosen:
                result = await runner.run(questions.draw(), args.effort)
                for slot_id in chosen:
                    slots[slot_id]['outcome'] = result.status
                atomic_json(path, journal)
                emit('window_slot_finished', slots=chosen, status=result.status, retry=False)
                if result.status not in ('success', 'high_demand', 'budget'):
                    return 1
        now = datetime.now(timezone.utc)
        # Keep at least two weekly periods of idempotency history, bounded on disk.
        cutoff = now - timedelta(days=30)
        journal['slots'] = {key: value for key, value in slots.items()
                            if instant(value['at']) >= cutoff or value['outcome'] == 'waiting'}
        atomic_json(path, journal)
        pending = [max(0, (instant(value['at']) - datetime.now(timezone.utc)).total_seconds())
                   for value in journal['slots'].values() if value['outcome'] == 'waiting']
        delay = min([max(0.05, next_poll - time.monotonic()), 30.0] + pending)
        await pause(asyncio.Event(), max(0.05, delay), max(0.05, delay))
    emit('stopped', reason='budget')
    return 0
