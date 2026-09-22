import assert from 'node:assert/strict';
import { test } from 'node:test';
import { installStaleChunkRecovery } from './stale-chunk-recovery.ts';

function tab(storage = new Map()) {
  const browser = new EventTarget();
  let reloads = 0;
  browser.sessionStorage = {
    getItem: (key) => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
  };
  browser.location = { reload: () => reloads++ };
  return { browser, reloads: () => reloads };
}

function emit(browser, type, reason) {
  const event = new Event(type, { cancelable: true });
  if (reason !== undefined) event.reason = reason;
  browser.dispatchEvent(event);
  return event;
}

test('preload and rejection notifications trigger only one reload', () => {
  const current = tab();
  installStaleChunkRecovery('build-a', current.browser);
  assert.equal(emit(current.browser, 'vite:preloadError').defaultPrevented, true);
  emit(current.browser, 'unhandledrejection', new TypeError('Failed to fetch dynamically imported module'));
  assert.equal(current.reloads(), 1);
});

test('a failed reload cannot loop, and a later build can recover in the same tab', () => {
  const storage = new Map();
  for (const [build, expected] of [['build-a', 1], ['build-a', 0], ['build-b', 1]]) {
    const current = tab(storage);
    installStaleChunkRecovery(build, current.browser);
    const event = emit(current.browser, 'vite:preloadError');
    assert.equal(current.reloads(), expected);
    assert.equal(event.defaultPrevented, expected === 1, 'unrecoverable errors must remain visible');
  }
});

test('only dynamic import rejections are recovered', () => {
  const current = tab();
  installStaleChunkRecovery('build-a', current.browser);
  assert.equal(emit(current.browser, 'unhandledrejection', new Error('ordinary failure')).defaultPrevented, false);
  assert.equal(current.reloads(), 0);
  assert.equal(emit(current.browser, 'unhandledrejection', new TypeError('Importing a module script failed.')).defaultPrevented, true);
  assert.equal(current.reloads(), 1);
});

test('blocked browser storage leaves the failure visible without a reload loop', () => {
  for (const operation of ['getItem', 'setItem']) {
    const current = tab();
    current.browser.sessionStorage[operation] = () => { throw new Error('storage blocked'); };
    installStaleChunkRecovery('build-a', current.browser);
    assert.equal(emit(current.browser, 'vite:preloadError').defaultPrevented, false);
    assert.equal(current.reloads(), 0);
  }
});
