'use client';

import { type ComponentProps, memo } from 'react';
import { Streamdown } from 'streamdown';
import { cn } from '@/lib/utils';
import { markdownRehypePlugins } from './markdown-plugins';

type ResponseProps = ComponentProps<typeof Streamdown>;

export const Response = memo(
  ({ className, ...props }: ResponseProps) => (
    <Streamdown
      rehypePlugins={markdownRehypePlugins}
      className={cn('size-full [&>*:first-child]:mt-0 [&>*:last-child]:mb-0', className)}
      shikiTheme={['one-light', 'one-dark-pro']}
      {...props}
    />
  ),
  (prevProps, nextProps) => prevProps.children === nextProps.children
);

Response.displayName = 'Response';
