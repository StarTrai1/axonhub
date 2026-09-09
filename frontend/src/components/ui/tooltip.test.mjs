import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

test('tooltip paragraphs use normal wrapping within their fitted width', () => {
  const source = readFileSync(new URL('./tooltip.tsx', import.meta.url), 'utf8');

  assert.match(source, /\bw-fit\b/);
  assert.match(source, /\btext-wrap\b/);
  assert.doesNotMatch(source, /\btext-balance\b/);
});

test('channel form descriptions do not reintroduce balanced paragraph wrapping', () => {
  const source = readFileSync(new URL('./form.tsx', import.meta.url), 'utf8');
  const description = source.slice(source.indexOf('function FormDescription('), source.indexOf('function FormMessage('));

  assert.doesNotMatch(description, /\btext-balance\b/);
});
