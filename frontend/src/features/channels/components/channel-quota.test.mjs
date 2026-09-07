import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('./codex-usage-cell.tsx', import.meta.url), 'utf8');
const ast = ts.createSourceFile('codex-usage-cell.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const helperNames = ['clampPercentage', 'remainingPercentage', 'findWindow'];
const helpers = ast.statements
  .filter((node) => ts.isFunctionDeclaration(node) && helperNames.includes(node.name?.text))
  .map((node) => 'export ' + node.getText(ast))
  .join('\n');
const { outputText } = ts.transpileModule(helpers, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
});
const { findWindow, remainingPercentage } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));
const hour = 3600;
const day = 24 * hour;

test('weekly-only primary quota is not relabeled as five-hour usage', () => {
  const weekly = { used_percent: 52, limit_window_seconds: 7 * day };
  const data = { rate_limit: { primary_window: weekly, secondary_window: null } };
  assert.equal(findWindow(data, 4 * hour, 6 * hour), undefined);
  assert.equal(findWindow(data, 6 * day, 8 * day), weekly);
  assert.equal(remainingPercentage(weekly), 48);
});

test('quota periods follow reported durations even when provider roles are reversed', () => {
  const weekly = { used_percent: 60, limit_window_seconds: 7 * day };
  const fiveHour = { used_percent: 20, limit_window_seconds: 5 * hour };
  for (const [primary, secondary] of [[fiveHour, weekly], [weekly, fiveHour]]) {
    const data = { rate_limit: { primary_window: primary, secondary_window: secondary } };
    assert.equal(findWindow(data, 4 * hour, 6 * hour), fiveHour);
    assert.equal(findWindow(data, 6 * day, 8 * day), weekly);
  }
});

test('missing and invalid windows do not fabricate an assumed quota period', () => {
  for (const duration of [undefined, null, 0, -1, NaN, Infinity, '604800']) {
    const data = { rate_limit: { primary_window: { used_percent: 52, limit_window_seconds: duration } } };
    assert.equal(findWindow(data, 4 * hour, 6 * hour), undefined);
    assert.equal(findWindow(data, 6 * day, 8 * day), undefined);
  }
  const legacy = { _limits: [{ window: 'primary', usageRatio: 0.52 }] };
  assert.equal(findWindow(legacy, 4 * hour, 6 * hour), undefined);
  assert.equal(findWindow(legacy, 6 * day, 8 * day), undefined);
});

test('remaining usage is clamped without presenting invalid numbers', () => {
  assert.equal(remainingPercentage({ used_percent: 150 }), 0);
  assert.equal(remainingPercentage({ used_percent: -10 }), 100);
  assert.equal(remainingPercentage({ used_percent: NaN }), undefined);
  assert.equal(remainingPercentage(undefined), undefined);
});

test('Codex quota remains owned by the health and usage column', () => {
  const columns = readFileSync(new URL('./channels-columns.tsx', import.meta.url), 'utf8');
  assert.match(columns, /CodexUsageCell channel=\{row.original\}/);
  assert.doesNotMatch(columns, /id:\s*['"]quota['"]/);
});
