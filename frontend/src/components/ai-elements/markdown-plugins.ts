import { defaultRehypePlugins } from 'streamdown';
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize';
import type { ComponentProps } from 'react';
import type { Streamdown } from 'streamdown';

// Sanitize untrusted HTML before KaTeX generates trusted MathML and styled
// spans. Sanitizing afterward strips formula markup and layout classes.
export const markdownRehypePlugins: ComponentProps<typeof Streamdown>['rehypePlugins'] = [
  defaultRehypePlugins.raw,
  [rehypeSanitize, {
    ...defaultSchema,
    attributes: {
      ...defaultSchema.attributes,
      code: [['className', /^language-./, 'math-inline', 'math-display']],
    },
  }],
  defaultRehypePlugins.katex,
  defaultRehypePlugins.harden,
];
