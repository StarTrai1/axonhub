import { useQuery } from '@tanstack/react-query';
import { ChevronRight, CircleHelp, ListFilter, Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { appPath } from '@/lib/app-path';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';

const catalogSchema = z.object({
  models: z.array(z.object({
    slug: z.string(),
    visibility: z.string().optional(),
    service_tiers: z.array(z.object({ id: z.string() })).nullish(),
    additional_speed_tiers: z.array(z.string()).nullish(),
  })),
});

async function loadKeyCatalog(key: string, signal: AbortSignal) {
  const response = await fetch(appPath('/codex/models'), {
    headers: { Authorization: `Bearer ${key}`, Originator: 'codex_cli_rs' },
    mode: 'same-origin', credentials: 'omit', redirect: 'error', cache: 'no-store', signal,
  });
  if (!response.ok) {
    const message = response.status === 404 ? (await response.json().catch(() => null))?.error?.message : '';
    if (message === 'Codex compatibility is disabled') throw new Error('disabled');
    throw new Error(String(response.status));
  }
  const { models } = catalogSchema.parse(await response.json());
  return { total: models.length, listed: models.filter((model) => model.visibility === 'list') };
}

export function CodexKeyCatalog({ keyId, apiKey }: { keyId: string; apiKey: string }) {
  const { t } = useTranslation();
  const query = useQuery({
    queryKey: ['codex-key-catalog', keyId],
    queryFn: ({ signal }) => loadKeyCatalog(apiKey, signal),
    enabled: false, retry: false, gcTime: 0,
  });
  const errorKeys: Record<string, string> = {
    disabled: 'disabled', '401': 'auth', '403': 'forbidden', '404': 'missing', '502': 'source', '503': 'source',
  };
  return (
    <div className='mb-3 space-y-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <Button type='button' variant='outline' size='sm' disabled={query.isFetching} onClick={() => void query.refetch()}>
          {query.isFetching ? <Loader2 className='size-4 animate-spin' /> : <ListFilter className='size-4' />}
          {t('apikeys.codexCatalog.check')}
        </Button>
        <Tooltip>
          <TooltipTrigger asChild>
            <button type='button' className='text-muted-foreground' aria-label={t('apikeys.codexCatalog.help')}>
              <CircleHelp className='size-4' />
            </button>
          </TooltipTrigger>
          <TooltipContent className='max-w-xs'>{t('apikeys.codexCatalog.help')}</TooltipContent>
        </Tooltip>
      </div>
      <div aria-live='polite' className='text-sm'>
        {query.isError ? (
          <p className='text-destructive'>{t(`apikeys.codexCatalog.${errorKeys[query.error.message] ?? 'failed'}`)}</p>
        ) : query.data && (
          <details className='group'>
            <summary className='flex cursor-pointer list-none items-center gap-1.5'>
              <ChevronRight className='size-4 shrink-0 group-open:rotate-90' />
              {t('apikeys.codexCatalog.result', { total: query.data.total, listed: query.data.listed.length })}
            </summary>
            {query.data.listed.length > 0 && (
              <ul className='mt-2 max-h-44 divide-y overflow-y-auto'>
                {query.data.listed.map(({ slug, service_tiers, additional_speed_tiers }) => {
                  const tiers = new Set([...(service_tiers ?? []).map(({ id }) => id), ...(additional_speed_tiers ?? [])]);
                  const speeds = [tiers.has('priority') || tiers.has('fast') ? 'Fast' : '', tiers.has('ultrafast') ? 'Ultrafast' : ''].filter(Boolean);
                  return (
                    <li key={slug} className='flex flex-wrap items-baseline justify-between gap-x-3 py-1'>
                      <span className='min-w-0 break-all'>{slug}</span>
                      {speeds.length > 0 && <span className='text-muted-foreground text-xs'>{speeds.join(' · ')}</span>}
                    </li>
                  );
                })}
              </ul>
            )}
          </details>
        )}
      </div>
    </div>
  );
}
