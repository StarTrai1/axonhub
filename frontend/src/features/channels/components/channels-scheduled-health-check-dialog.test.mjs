import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { format } from 'date-fns';
import ts from 'typescript';

const source = readFileSync(new URL('./channels-scheduled-health-check-dialog.tsx', import.meta.url), 'utf8');
const ast = ts.createSourceFile('scheduled-health.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const formatter = ast.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === 'formatScheduleTimestamp');
assert.ok(formatter);
const javascript = ts.transpileModule(formatter.getText(ast), {
  compilerOptions: { target: ts.ScriptTarget.ES2023 },
}).outputText;
const formatTimestamp = new Function('format', `${javascript}; return formatScheduleTimestamp;`)(format);

test('schedule status timestamps preserve seconds and reject absent or invalid values', () => {
  for (const value of [null, undefined, '', 'invalid', '0001-01-01T00:00:00Z']) {
    assert.equal(formatTimestamp(value), '—');
  }
  const value = '2026-09-10T09:00:01+08:00';
  assert.equal(formatTimestamp(value), format(new Date(value), 'yyyy-MM-dd HH:mm:ss'));
});

test('runtime refresh does not reset unsaved schedule edits', () => {
  assert.match(source, /JSON\.stringify\(data\.times\)/);
  assert.match(source, /\[savedTimes, open\]/);
  assert.doesNotMatch(source, /\[data, open\]/);
  for (const field of ['nextRunAt', 'lastDispatchAt', 'scheduledFor', 'startedAt', 'checkpointError']) {
    assert.ok(source.includes(field));
  }
});

test('scheduled health status copy is available in both locales', () => {
  const keys = [...source.matchAll(/t\('(channels\.dialogs\.scheduledHealthCheck\.[^']+)'/g)].map((match) => match[1]);
  for (const language of ['en', 'zh-CN']) {
    const translations = JSON.parse(readFileSync(new URL(`../../../locales/${language}/channels.json`, import.meta.url), 'utf8'));
    for (const key of keys) assert.equal(typeof translations[key], 'string', `${language}: ${key}`);
    for (const state of ['never', 'running', 'succeeded', 'failed', 'interrupted']) {
      assert.equal(typeof translations[`channels.dialogs.scheduledHealthCheck.states.${state}`], 'string');
    }
  }
});
