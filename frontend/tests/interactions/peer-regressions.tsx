import { StrictMode, useMemo, useState, type ComponentProps } from 'react';
import { createRoot } from 'react-dom/client';
import { useForm, type Control } from 'react-hook-form';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '@/index.css';
import '@/lib/i18n';
import { Form } from '@/components/ui/form';
import { FilePreview } from '@/components/ui/file-preview';
import { ModelPriceEditor } from '@/components/model-price-editor';
import { PriceScheduleEditor } from '@/components/price-schedule-editor';
import { BrandSettings } from '@/features/system/components/brand-settings';
import SystemProvider from '@/features/system/context/system-context';
import ProfileForm from '@/features/settings/profile/profile-form';

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

const fixture = new URLSearchParams(window.location.search).get('case') === 'uploads' ? <UploadFixture /> : <PriceFixture />;
createRoot(document.getElementById('root')!).render(
  <StrictMode><QueryClientProvider client={queryClient}>{fixture}</QueryClientProvider></StrictMode>
);
