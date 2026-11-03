import type { ComponentProps } from 'react';
import { cjk } from '@streamdown/cjk';
import { code, createCodePlugin } from '@streamdown/code';
import { math } from '@streamdown/math';
import { mermaid } from '@streamdown/mermaid';
import 'katex/dist/katex.min.css';
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize';
import { defaultRehypePlugins } from 'streamdown';
import type { Streamdown } from 'streamdown';

// Streamdown 2 appends its math renderer after these rehype plugins, so
// untrusted HTML is sanitized before KaTeX generates trusted MathML/spans.
export const markdownRehypePlugins: ComponentProps<typeof Streamdown>['rehypePlugins'] = [
  defaultRehypePlugins.raw,
  [
    rehypeSanitize,
    {
      ...defaultSchema,
      attributes: {
        ...defaultSchema.attributes,
        code: [['className', /^language-./, 'math-inline', 'math-display']],
      },
    },
  ],
  defaultRehypePlugins.harden,
];

export const markdownPlugins = { code, math, mermaid, cjk };
export const themedMarkdownPlugins = {
  ...markdownPlugins,
  code: createCodePlugin({ themes: ['one-light', 'one-dark-pro'] }),
};
