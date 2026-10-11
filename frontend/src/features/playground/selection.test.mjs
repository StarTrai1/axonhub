import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import { defaultSelection, readSelection, resolveSelection, writeSelection } from './selection.ts';

const savedStorage = globalThis.localStorage;
afterEach(() => {
  globalThis.localStorage = savedStorage;
});

function storage() {
  const values = new Map();
  globalThis.localStorage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
  return values;
}

test('stores only the selection for its project', () => {
  const values = storage();
  const selected = { modelSource: 'channel', selectedChannel: 'second', model: 'model-b' };
  writeSelection('project-a', selected, 'user-a');
  assert.deepEqual(readSelection('project-a', 'user-a'), selected);
  assert.deepEqual(readSelection('project-b', 'user-a'), defaultSelection);
  assert.deepEqual(JSON.parse(values.get('axonhub_playground_selection_v2_["user-a","project-a"]')), selected);
});

test('corrupt and unavailable storage fall back without throwing', () => {
  const values = storage();
  values.set('axonhub_playground_selection_v2_["user-a","project-a"]', '{broken');
  assert.deepEqual(readSelection('project-a', 'user-a'), defaultSelection);
  values.set('axonhub_playground_selection_v2_["user-a","project-a"]', JSON.stringify({ modelSource: 'unknown', model: 'a' }));
  assert.deepEqual(readSelection('project-a', 'user-a'), defaultSelection);
  globalThis.localStorage = {
    getItem: () => {
      throw new Error('denied');
    },
    setItem: () => {
      throw new Error('denied');
    },
  };
  assert.deepEqual(readSelection('project-a', 'user-a'), defaultSelection);
  assert.doesNotThrow(() => writeSelection('project-a', defaultSelection, 'user-a'));
});

const channels = [
  { value: 'first', models: ['first-a'] },
  { value: 'second', models: ['second-a', 'second-b'] },
];

test('restores a valid channel and model and falls back when either disappears', () => {
  const selected = { modelSource: 'channel', selectedChannel: 'second', model: 'second-b' };
  assert.deepEqual(resolveSelection(selected, channels, [], false), selected);
  assert.deepEqual(resolveSelection({ ...selected, model: 'deleted' }, channels, [], false), { ...selected, model: 'second-a' });
  assert.deepEqual(resolveSelection(selected, channels.slice(0, 1), [], false), {
    modelSource: 'channel',
    selectedChannel: 'first',
    model: 'first-a',
  });
  assert.deepEqual(resolveSelection(selected, [], [], false), defaultSelection);
});

test('gateway model survives with access and falls back without access', () => {
  const selected = { modelSource: 'model_gateway', selectedChannel: 'second', model: 'gateway-b' };
  assert.deepEqual(resolveSelection(selected, channels, ['gateway-a', 'gateway-b'], true), selected);
  assert.deepEqual(resolveSelection(selected, channels, ['gateway-a'], true), { ...selected, model: 'gateway-a' });
  assert.deepEqual(resolveSelection(selected, channels, ['gateway-a'], false), {
    modelSource: 'channel',
    selectedChannel: 'second',
    model: 'second-a',
  });
});


test('isolates accounts in the same project and ignores ownerless legacy state', () => {
  const values = storage();
  const selected = { modelSource: 'channel', selectedChannel: 'private', model: 'model-a' };
  values.set('axonhub_playground_selection_project-a', JSON.stringify(selected));
  assert.deepEqual(readSelection('project-a', 'user-b'), defaultSelection);
  writeSelection('project-a', selected, 'user-a');
  assert.deepEqual(readSelection('project-a', 'user-b'), defaultSelection);
  assert.deepEqual(readSelection('project-a', 'user-a'), selected);
  assert.deepEqual(readSelection('project-a', null), defaultSelection);
  const count = values.size;
  writeSelection('project-a', selected, null);
  writeSelection(null, selected, 'user-a');
  assert.equal(values.size, count);
});
