import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import { providersDataSchema } from './providers.schema.ts';

const providersJsonPath = join(import.meta.dirname, 'providers.json');

function formatIssue(issue) {
  const path = issue.path.length > 0 ? issue.path.join('.') : '<root>';
  return `  ${path}: ${issue.message}`;
}

test('bundled providers.json matches providersDataSchema', () => {
  const raw = JSON.parse(readFileSync(providersJsonPath, 'utf8'));
  const result = providersDataSchema.safeParse(raw);

  if (!result.success) {
    const issues = result.error.issues;
    const shown = issues.slice(0, 10).map(formatIssue).join('\n');
    const rest = issues.length > 10 ? `\n  ... and ${issues.length - 10} more` : '';
    assert.fail(
      `providers.json (synced by scripts/sync/sync-model-developers.js) no longer matches the schema that providers.ts parses at import time.\nUpdate providers.schema.ts to accept the new shape:\n${shown}${rest}`
    );
  }

  assert.ok(Object.keys(result.data.providers).length > 0, 'bundled providers.json must contain at least one provider');
});

test('GPT-6.1 Sol and Astra modes preserve official prices across both catalogs', () => {
  const frontend = providersDataSchema.parse(JSON.parse(readFileSync(providersJsonPath, 'utf8')));
  const backend = JSON.parse(readFileSync(join(import.meta.dirname, '../../../../../internal/server/biz/catalogdata/providers.json'), 'utf8'));
  const sol = frontend.providers.openai.models.find((model) => model.id === 'gpt-6.1-sol');
  assert.ok(sol);
  assert.deepEqual(sol.reasoning_options[0].values, ['low', 'medium', 'high', 'xhigh', 'max']);
  assert.equal(sol.cost.input, 2);
  assert.equal(sol.cost.cache_read, 0.1);
  assert.equal(sol.cost.cache_write, 2.5);
  assert.equal(sol.cost.output, 10);
  assert.equal(sol.cost.tiers[0].tier.size, 272000);
  assert.equal(sol.experimental.modes.fast.cost.tiers[0].output, 30);
  assert.equal(sol.experimental.modes.ultrafast, undefined);
  const astra = frontend.providers.openai.models.find((model) => model.id === 'gpt-6-astra');
  assert.equal(astra.experimental.modes.ultrafast.provider.body.service_tier, 'ultrafast');
  assert.equal(astra.experimental.modes.ultrafast.cost.input, 60);
  assert.equal(astra.experimental.modes.ultrafast.cost.tiers[0].output, 450);
  for (const model of [sol, astra]) {
    const source = backend.providers.openai.models.find((entry) => entry.id === model.id);
    assert.deepEqual(model, providersDataSchema.parse({ providers: { openai: { ...backend.providers.openai, models: [source] } } }).providers.openai.models[0]);
  }
});
