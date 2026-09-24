import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { flexRender, getCoreRowModel, useReactTable } from '@tanstack/react-table';
import '@/index.css';
import '@/lib/i18n';
import { TooltipProvider } from '@/components/ui/tooltip';
import { useRequestsColumns } from '@/features/requests/components/requests-columns';
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
  routeTree: createRootRoute({ component: RequestModelAuditFixture }),
  history: createMemoryHistory({ initialEntries: ['/'] }),
});
createRoot(document.getElementById('root')!).render(
  <StrictMode><QueryClientProvider client={queryClient}><TooltipProvider>
    <RouterProvider router={router} />
  </TooltipProvider></QueryClientProvider></StrictMode>
);
