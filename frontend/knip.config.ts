import type { KnipConfig } from 'knip';

const config: KnipConfig = {
  entry: ['src/main.tsx', 'src/routes/**/*.{ts,tsx}', 'tests/interactions/*.tsx', 'tests/**/*.spec.ts', '*.config.{ts,js,mjs}', 'src/**/*.test.mjs'],
  ignore: ['src/components/ui/**', 'src/routeTree.gen.ts'],
  ignoreDependencies: ["tailwindcss", "tw-animate-css"]
};

export default config;