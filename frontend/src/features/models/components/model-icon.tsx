import { lazy, Suspense, type ComponentType } from 'react';

type IconProps = { className?: string };

// Keep the complete installed icon catalog available without importing every
// brand and its variants into the model table's initial chunk.
const modules = import.meta.glob<{ default: ComponentType<IconProps> }>('../../../../node_modules/@lobehub/icons/es/*/index.js');
const icons = new Map(Object.entries(modules).map(([path, load]) => [path.split('/').at(-2)!, lazy(load)]));

export function ModelIcon({ name }: { name?: string | null }) {
  const Icon = name ? icons.get(name) : undefined;
  if (!Icon) return <span className='text-muted-foreground text-xs'>-</span>;
  return (
    <Suspense fallback={<span aria-hidden className='inline-block h-5 w-5' />}>
      <Icon className='h-5 w-5' />
    </Suspense>
  );
}
