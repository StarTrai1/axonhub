import { StrictMode, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { Toaster } from 'sonner';
import '@/index.css';
import '@/lib/i18n';
import { TooltipProvider } from '@/components/ui/tooltip';
import ChannelsProvider, { useChannels } from '@/features/channels/context/channels-context';
import { ChannelsTable } from '@/features/channels/components/channels-table';
import { createColumns } from '@/features/channels/components/channels-columns';
import { ChannelsSystemSettingsDialog } from '@/features/channels/components/channels-system-settings-dialog';
import { ChannelsBulkEnableDialog } from '@/features/channels/components/channels-bulk-enable-dialog';
import type { Channel } from '@/features/channels/data/schema';

const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
queryClient.setQueryData(['me', ''], {
  id: 'fixture-user', email: 'my@example.com', firstName: 'Offline', lastName: 'User',
  isOwner: true, preferLanguage: 'en', scopes: [], roles: [], projects: [],
});
queryClient.setQueryData(['channelSetting'], { probe: { enabled: false, frequency: 'ONE_MINUTE' } });
queryClient.setQueryData(['allChannelTags', undefined], []);
const data = ['disabled', 'enabled', 'archived'].map((status, index) => ({
  id: `channel-${index}`, name: status, status, type: 'openai', supportedModels: [], tags: [],
}) as Channel);
const noop = () => {};

function Fixture() {
  const { t } = useTranslation();
  const { setOpen } = useChannels();
  const columns = useMemo(() => createColumns(t).filter((column) => column.id === 'select' ||
    ('accessorKey' in column && column.accessorKey === 'status')), [t]);
  const [columnVisibility, setColumnVisibility] = useState({});
  return <>
    <button data-testid='open-settings' onClick={() => setOpen('channelSettings')}>Settings</button>
    <ChannelsTable columns={columns} data={data} pageSize={10} nameFilter='' typeFilter={[]} statusFilter={[]}
      tagFilter='' modelFilter='' sorting={[]} onSortingChange={noop} onNextPage={noop} onPreviousPage={noop}
      onPageSizeChange={noop} onNameFilterChange={noop} onTypeFilterChange={noop} onStatusFilterChange={noop}
      onTagFilterChange={noop} onModelFilterChange={noop} columnVisibility={columnVisibility}
      onColumnVisibilityChange={setColumnVisibility} />
    <ChannelsSystemSettingsDialog />
    <ChannelsBulkEnableDialog />
    <Toaster />
  </>;
}
const router = createRouter({
  routeTree: createRootRoute({ component: () => <ChannelsProvider><Fixture /></ChannelsProvider> }),
  history: createMemoryHistory({ initialEntries: ['/'] }),
});
createRoot(document.getElementById('root')!).render(
  <StrictMode><QueryClientProvider client={queryClient}><TooltipProvider>
    <RouterProvider router={router} />
  </TooltipProvider></QueryClientProvider></StrictMode>
);
