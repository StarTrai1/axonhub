import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { flexRender, getCoreRowModel, useReactTable } from '@tanstack/react-table';
import '@/index.css';
import i18n from '@/lib/i18n';
import { ThemeProvider } from '@/context/theme-context';
import { TooltipProvider } from '@/components/ui/tooltip';
import { useRequestsColumns } from '@/features/requests/components/requests-columns';
import { RequestDetailContent } from '@/features/requests/components/request-detail-content';
import type { Request } from '@/features/requests/data/schema';

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
queryClient.setQueryData(['generalSettings'], {});
queryClient.setQueryData(['securitySettings'], { blockedIPs: [] });

const scenarios = ['pending', 'processing', 'failed', 'canceled', 'unknown', 'matched', 'mismatched'] as const;
const data: Request[] = scenarios.map((scenario) => {
  const status = ['unknown', 'matched', 'mismatched'].includes(scenario) ? 'completed' : scenario;
  return {
    id: scenario, modelID: 'gpt-6-sol', status, source: 'api', stream: true,
    createdAt: new Date(), updatedAt: new Date(), format: 'openai/responses',
    executions: { edges: scenario === 'unknown' ? [] : [{ cursor: scenario, node: {
      modelID: 'gpt-6-sol', status,
      upstreamModelID: scenario === 'mismatched' ? 'gpt-6-luna' : 'gpt-6-sol',
      format: 'openai/responses',
    } }] },
  } as Request;
});

function RequestModelAuditFixture() {
  // Render the real column callback, rather than a reimplementation of the verdict.
  const columns = useRequestsColumns().filter((column) => column.id === 'modelID');
  const table = useReactTable({ data, columns, getCoreRowModel: getCoreRowModel() });
  return <table><tbody>{table.getRowModel().rows.map((row) => (
    <tr key={row.id} data-testid={`request-${row.original.id}`}>
      {row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}
    </tr>
  ))}</tbody></table>;
}

const router = createRouter({
  routeTree: createRootRoute({ component: () => new URLSearchParams(location.search).has('quota')
    ? <RequestDetailContent requestId='80853' /> : <RequestModelAuditFixture /> }),
  history: createMemoryHistory({ initialEntries: ['/'] }),
});
const params = new URLSearchParams(location.search);
if (params.has('quota')) await i18n.changeLanguage('zh-CN');
createRoot(document.getElementById('root')!).render(
  <StrictMode><QueryClientProvider client={queryClient}><TooltipProvider>
    <ThemeProvider defaultTheme={params.get('theme') === 'dark' ? 'dark' : 'light'} defaultColorScheme='claude'>
      <RouterProvider router={router} />
    </ThemeProvider>
  </TooltipProvider></QueryClientProvider></StrictMode>
);
