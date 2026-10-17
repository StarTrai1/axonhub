import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import '@/index.css';
import '@/lib/i18n';
import { ThemeProvider, useTheme } from '@/context/theme-context';
import { TooltipProvider } from '@/components/ui/tooltip';
import { ChannelsActionDialog } from '@/features/channels/components/channels-action-dialog';
import { channelSchema, type Channel } from '@/features/channels/data/schema';

const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
queryClient.setQueryData(['me', ''], { id: 'fixture', email: 'fixture@example.test', firstName: 'Fixture', lastName: 'User', isOwner: false, scopes: [], roles: [], projects: [] });
queryClient.setQueryData(['allChannelTags', undefined], []);
queryClient.setQueryData(['proxyPresets'], []);
const row = channelSchema.parse({
  id: 'channel-preview', createdAt: '2026-10-17T00:00:00Z', updatedAt: '2026-10-17T00:00:00Z',
  name: 'Codex fixture', type: 'codex', status: 'enabled', baseURL: 'https://chatgpt.com/backend-api/codex#',
  credentials: { apiKey: JSON.stringify({ access_token: 'synthetic', refresh_token: 'synthetic' }) },
  policies: { webSearch: 'auto' },
  supportedModels: ['gpt-6-sol', 'gpt-5.5'], defaultTestModel: 'gpt-6-sol', settings: { codexSearchOnly: false },
});
function Fixture() {
  const {setTheme} = useTheme();
  const [open, setOpen] = useState(true);
  const [currentRow, setCurrentRow] = useState<Channel>(row);
  return <>
    <button data-testid="dark-theme" onClick={() => setTheme("dark")}>Dark theme</button>
    <button data-testid='reopen-codex' onClick={async () => {
      const response = await fetch('/fixture/channel');
      setCurrentRow(channelSchema.parse(await response.json()));
      setOpen(true);
    }}>Reopen saved channel</button>
    {open && <ChannelsActionDialog currentRow={currentRow} open={open} onOpenChange={setOpen} />}
  </>;
}
const router = createRouter({ routeTree: createRootRoute({ component: Fixture }), history: createMemoryHistory({ initialEntries: ['/'] }) });
createRoot(document.getElementById('root')!).render(<StrictMode><QueryClientProvider client={queryClient}><ThemeProvider defaultTheme="light" defaultColorScheme="claude"><TooltipProvider><RouterProvider router={router} /></TooltipProvider></ThemeProvider></QueryClientProvider></StrictMode>);
