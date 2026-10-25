import { StrictMode, useMemo, useState, type ComponentProps } from 'react';
import { createRoot } from 'react-dom/client';
import { useForm, type Control } from 'react-hook-form';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '@/index.css';
import '@/lib/i18n';
import { Form } from '@/components/ui/form';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { AutoComplete } from '@/components/auto-complete';
import { AutoCompleteSelect } from '@/components/auto-complete-select';
import { Response } from '@/components/ai-elements/response';
import { FilePreview } from '@/components/ui/file-preview';
import { ModelPriceEditor } from '@/components/model-price-editor';
import { PriceScheduleEditor } from '@/components/price-schedule-editor';
import { BrandSettings } from '@/features/system/components/brand-settings';
import SystemProvider from '@/features/system/context/system-context';
import ProfileForm from '@/features/settings/profile/profile-form';
import Playground from '@/features/playground';
import { useAuthStore } from '@/stores/authStore';
import { useProjectStore } from '@/stores/projectStore';

type ModelValues = ComponentProps<typeof ModelPriceEditor>['control'] extends Control<infer Values> ? Values : never;
type ScheduleValues = ComponentProps<typeof PriceScheduleEditor>['control'] extends Control<infer Values> ? Values : never;

const tiers = [{ upTo: 100, pricePerUnit: '1' }, { upTo: null, pricePerUnit: '2' }];
const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
queryClient.setQueryData(['brandSettings'], { brandName: 'Fixture', title: 'Offline', brandLogo: '' });
queryClient.setQueryData(['me', ''], {
  id: 'fixture-user', email: 'my@example.com', firstName: 'Offline', lastName: 'User',
  isOwner: false, preferLanguage: 'en', avatar: '', scopes: [], roles: [], projects: [],
});

function PriceFixture() {
  const modelForm = useForm<ModelValues>({
    defaultValues: { prices: [{ modelId: 'fixture', price: { items: [
      { itemCode: 'prompt_tokens', pricing: { mode: 'usage_tiered', usageTiered: { tiers } } },
      { itemCode: 'prompt_write_cached_tokens', pricing: { mode: 'usage_per_unit', usagePerUnit: '1' },
        promptWriteCacheVariants: [{ variantCode: 'five_min', pricing: { mode: 'usage_tiered', usageTiered: { tiers } } }] },
    ] } }] },
  });
  const scheduleForm = useForm<ScheduleValues>({
    defaultValues: { prices: [{ modelId: 'fixture', price: {
      items: [], schedule: { timezone: 'UTC', overrides: [{
        name: 'Fixture', priority: 0, when: { weekdays: [1] },
        items: [{ itemCode: 'prompt_tokens', pricing: { mode: 'usage_tiered', usageTiered: { tiers } } }],
      }] },
    } }] },
  });
  return <>
    <section data-testid='model-prices'>
      <Form {...modelForm}>
        <ModelPriceEditor control={modelForm.control} priceIndex={0} currencyCode='USD'
          onAddItem={() => {}} onRemoveItem={() => {}} onAddVariant={() => {}} onRemoveVariant={() => {}} />
      </Form>
      <output data-testid='model-values'>{JSON.stringify(modelForm.watch())}</output>
    </section>
    <section data-testid='scheduled-prices'>
      <Form {...scheduleForm}><PriceScheduleEditor control={scheduleForm.control} priceIndex={0} currencyCode='USD' /></Form>
      <output data-testid='schedule-values'>{JSON.stringify(scheduleForm.watch())}</output>
    </section>
  </>;
}

function UploadFixture() {
  const [fileName, setFileName] = useState('first.txt');
  const [mounted, setMounted] = useState(true);
  const file = useMemo(() => new File(['offline'], fileName, { type: 'text/plain' }), [fileName]);
  return <>
    <button data-testid='next-file' onClick={() => setFileName('second.txt')}>Select another file</button>
    <button data-testid='unmount-uploads' onClick={() => setMounted(false)}>Unmount uploads</button>
    {mounted && <>
      <section data-testid='text-preview'><FilePreview file={file} /></section>
      <section data-testid='brand'><SystemProvider><BrandSettings /></SystemProvider></section>
      <section data-testid='profile'><ProfileForm /></section>
    </>}
  </>;
}

function PlaygroundFixture() {
  return <>
    <button data-testid='switch-playground-user' onClick={() => {
      const current = useAuthStore.getState().auth.user!;
      useAuthStore.getState().auth.setUser({ ...current, id: 'second-user' });
    }}>Switch user</button>
    <button data-testid='switch-playground-project' onClick={() => useProjectStore.getState().setSelectedProjectId('second-project')}>Switch project</button>
    <Playground />
  </>;
}

const fixtureCase = new URLSearchParams(window.location.search).get('case');
function CompositionFixture() {
  const [selected, setSelected] = useState('');
  const [strictSelected, setStrictSelected] = useState('');
  const [search, setSearch] = useState('');
  const [keys, setKeys] = useState(0);
  const items = [{ value: 'first', label: 'First' }, { value: 'second', label: 'Second' }];
  return <>
    <section data-testid='composition-free'>
      <AutoComplete selectedValue={selected} onSelectedValueChange={setSelected} searchValue={search}
        onSearchValueChange={setSearch} items={items} onKeyDown={() => setKeys((n) => n + 1)} />
      <output data-testid='composition-free-value'>{selected}</output>
      <output data-testid='composition-key-count'>{keys}</output>
    </section>
    <section data-testid='composition-strict'>
      <AutoCompleteSelect selectedValue={strictSelected} onSelectedValueChange={setStrictSelected} items={items} />
      <output data-testid='composition-strict-value'>{strictSelected}</output>
    </section>
  </>;
}
function RenderingFixture() {
  return <section data-testid='rendering-response'><Response>{'## Formula\n\n$$E = mc^2$$\n\n<script>window.renderingInjected = true</script>\n\n<img src="invalid:" onerror="window.renderingInjected = true">\n\n```mermaid\nflowchart LR\nA[Input] --> B[Output]\n```'}</Response></section>;
}
function ServerSelectFixture() {
  const [selected, setSelected] = useState('');
  const [search, setSearch] = useState('');
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  const items = search === '' || search === 'fixture' ? [{ value: 'user-1', label: 'Fixture User (my@example.com)' }] : [];
  return <Dialog open>
    <DialogContent ref={setContainer} data-testid='server-select-dialog'>
      <DialogTitle>Choose a user</DialogTitle>
      <DialogDescription>Offline server search fixture</DialogDescription>
      <label htmlFor='server-user'>User</label>
      <AutoCompleteSelect selectedValue={selected} onSelectedValueChange={setSelected} items={items}
        searchValue={search} onSearchValueChange={setSearch} portalContainer={container}
        inputProps={{ id: 'server-user', 'aria-describedby': 'server-user-help' }} />
      <span id='server-user-help'>Choose an existing account</span>
      <button data-testid='clear-server-selection' onClick={() => setSelected('')}>Clear selection</button>
      <output data-testid='server-selection'>{selected}</output>
    </DialogContent>
  </Dialog>;
}
if (fixtureCase === 'playground') {
  useAuthStore.getState().auth.setUser({
    id: 'first-user', email: 'my@example.com', firstName: 'Offline', lastName: 'User',
    isOwner: false, preferLanguage: 'en', scopes: [], roles: [], projects: [],
  });
  useProjectStore.getState().setSelectedProjectId('first-project');
}
const fixture = fixtureCase === 'server-select' ? <ServerSelectFixture /> : fixtureCase === 'rendering' ? <RenderingFixture /> : fixtureCase === 'composition' ? <CompositionFixture /> : fixtureCase === 'uploads' ? <UploadFixture /> : fixtureCase === 'playground' ? <PlaygroundFixture /> : <PriceFixture />;
createRoot(document.getElementById('root')!).render(
  <StrictMode><QueryClientProvider client={queryClient}>{fixture}</QueryClientProvider></StrictMode>
);
