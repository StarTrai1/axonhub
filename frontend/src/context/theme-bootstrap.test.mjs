import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { runInNewContext } from 'node:vm';

const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
const bootstrap = html.match(/<script>([\s\S]*?)<\/script>/)[1];

test('first paint respects saved preferences and survives unavailable or invalid storage', () => {
  for (const [saved, systemDark, expected] of [
    [{ 'axonhub-ui-theme': 'light', 'axonhub-ui-color-scheme': 'starry' }, true, ['light', 'starry']],
    [{ 'axonhub-ui-theme': 'dark', 'axonhub-ui-color-scheme': 'blue' }, false, ['dark', 'blue']],
    [{ 'axonhub-ui-theme': 'system' }, true, ['dark', 'claude']],
    [{ 'axonhub-ui-theme': 'invalid', 'axonhub-ui-color-scheme': 'invalid' }, false, ['light', 'claude']],
    [null, true, ['dark', 'claude']],
  ]) {
    const classes = [];
    runInNewContext(bootstrap, {
      localStorage: {
        getItem(key) {
          if (saved === null) throw new Error('storage unavailable');
          return saved[key] ?? null;
        },
      },
      matchMedia: () => ({ matches: systemDark }),
      document: { documentElement: { classList: { add: (...values) => classes.push(...values) } } },
    });
    assert.deepEqual(classes, expected);
  }
});
