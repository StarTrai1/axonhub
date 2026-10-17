import assert from 'node:assert/strict';
import test from 'node:test';
import { isCodexSearchModel, codexSearchModelOptions } from './codex-search.ts';

test('search model choices exclude old models and arbitrary aliases', () => {
  const options = codexSearchModelOptions(['gpt-5.5', 'gpt-5.6-sol', 'gpt-7-sol', 'random-model'], 'gpt-6.2-sol');
  assert.ok(options.includes('gpt-7-sol'));
  assert.ok(options.includes('gpt-6.2-sol'));
  assert.ok(!options.includes('gpt-5.5'));
  assert.ok(!options.includes('random-model'));
  assert.equal(options.filter((model) => model === 'gpt-5.6-sol').length, 1);
});

test('search model validation accepts version numbers rather than lexicographic order', () => {
  for (const model of ['gpt-5.6-sol', 'gpt-5.10-sol', 'gpt-6.1-sol', 'gpt-7']) assert.ok(isCodexSearchModel(model), model);
  for (const model of ['gpt-5.5', 'gpt-5-sol', 'gpt-5.6oops', 'gpt-6/../../x', 'alias']) assert.ok(!isCodexSearchModel(model), model);
});
