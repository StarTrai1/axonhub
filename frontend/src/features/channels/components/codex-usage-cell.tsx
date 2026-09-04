import { format } from 'date-fns';
import { Clock3 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import type { ProviderCodexQuotaData } from '@/features/system/data/quotas';
import type { Channel } from '../data/schema';

type CodexQuotaWindow = NonNullable<NonNullable<ProviderCodexQuotaData['rate_limit']>['primary_window']>;

const HOUR_SECONDS = 60 * 60;
const DAY_SECONDS = 24 * HOUR_SECONDS;

function clampPercentage(value: number): number {
  return Math.min(100, Math.max(0, value));
}

function remainingPercentage(window: CodexQuotaWindow | undefined): number | undefined {
  if (typeof window?.used_percent !== 'number' || !Number.isFinite(window.used_percent)) return undefined;
  return clampPercentage(100 - window.used_percent);
}

function findWindow(
  quotaData: ProviderCodexQuotaData,
  minimumDurationSeconds: number,
  maximumDurationSeconds: number
): CodexQuotaWindow | undefined {
  const rateLimit = quotaData.rate_limit;
  return [rateLimit?.primary_window, rateLimit?.secondary_window].find((window) => {
    const duration = window?.limit_window_seconds;
    return typeof duration === 'number' && duration >= minimumDurationSeconds && duration <= maximumDurationSeconds;
  });
}

function formatUnixTimestamp(timestamp: number | undefined): string | undefined {
  if (!timestamp) return undefined;
  const date = new Date(timestamp * 1000);
  if (Number.isNaN(date.getTime())) return undefined;
  return format(date, 'yyyy-MM-dd HH:mm');
}

function formatISOString(timestamp: string | undefined | null): string | undefined {
  if (!timestamp) return undefined;
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return undefined;
  return format(date, 'yyyy-MM-dd HH:mm');
}

function meterColor(remaining: number | undefined): string {
  if (remaining === undefined) return 'bg-muted-foreground/30';
  if (remaining <= 20) return 'bg-red-500';
  if (remaining <= 50) return 'bg-amber-500';
  return 'bg-emerald-500';
}

function QuotaMeter({ label, window }: { label: string; window: CodexQuotaWindow | undefined }) {
  const remaining = remainingPercentage(window);

  return (
    <div className='grid grid-cols-[2.25rem_1fr_2.5rem] items-center gap-1.5 text-[11px] leading-none'>
      <span className='text-muted-foreground font-medium'>{label}</span>
      <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
        <div
          className={cn('h-full rounded-full transition-[width] duration-300', meterColor(remaining))}
          style={{ width: `${remaining ?? 0}%` }}
        />
      </div>
      <span className='text-foreground text-right font-semibold tabular-nums'>{remaining === undefined ? '—' : `${Math.round(remaining)}%`}</span>
    </div>
  );
}

function QuotaDetail({ label, window }: { label: string; window: CodexQuotaWindow | undefined }) {
  const { t } = useTranslation();
  const remaining = remainingPercentage(window);
  const resetAt = formatUnixTimestamp(window?.reset_at);

  return (
    <div className='space-y-1.5'>
      <div className='flex items-center justify-between gap-4 text-xs'>
        <span className='font-medium'>{label}</span>
        <span className='tabular-nums'>
          {remaining === undefined
            ? t('channels.codexUsage.noActiveWindow')
            : t('channels.codexUsage.remainingValue', { percent: Math.round(remaining) })}
        </span>
      </div>
      {remaining !== undefined && (
        <div className='text-background/80 flex items-center justify-between gap-4 text-[11px]'>
          <span>{t('channels.codexUsage.usedValue', { percent: Math.round(100 - remaining) })}</span>
          <span>{resetAt ? t('channels.codexUsage.resetsAt', { time: resetAt }) : t('channels.codexUsage.resetUnknown')}</span>
        </div>
      )}
    </div>
  );
}

export function CodexUsageCell({ channel }: { channel: Channel }) {
  const { t } = useTranslation();
  const quotaStatus = channel.providerQuotaStatus;
  const quotaData = (quotaStatus?.quotaData ?? {}) as ProviderCodexQuotaData;
  const fiveHourWindow = findWindow(quotaData, 4 * HOUR_SECONDS, 6 * HOUR_SECONDS);
  const weeklyWindow = findWindow(quotaData, 6 * DAY_SECONDS, 8 * DAY_SECONDS);
  const resetCredits = quotaData.rate_limit_reset_credits;
  const resetDetails = quotaData._resets?.resets ?? [];
  const resetCreditCount = quotaData._resets?.availableCount ?? resetCredits?.available_count;
  const hasQuotaData = Boolean(fiveHourWindow || weeklyWindow);

  if (!quotaStatus || quotaStatus.providerType !== 'codex' || quotaData.error) {
    const pendingText =
      channel.status === 'enabled' ? t('channels.codexUsage.pending') : t('channels.codexUsage.enableToRefresh');
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge variant='outline' className='text-muted-foreground h-6 gap-1 whitespace-nowrap font-normal'>
            <Clock3 className='h-3 w-3' />
            {t('channels.codexUsage.waiting')}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>{pendingText}</TooltipContent>
      </Tooltip>
    );
  }

  const lastUpdated = formatISOString(quotaStatus.updatedAt);
  const nextCheck = formatISOString(quotaStatus.nextCheckAt);

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className='hover:bg-muted/50 w-36 cursor-default space-y-1.5 rounded-md border px-2 py-1.5 transition-colors'>
          <QuotaMeter label='5h' window={fiveHourWindow} />
          <QuotaMeter label={t('channels.codexUsage.weekShort')} window={weeklyWindow} />
        </div>
      </TooltipTrigger>
      <TooltipContent side='left' className='text-background w-72 space-y-3 p-3'>
        <div className='flex items-center justify-between gap-3'>
          <span className='font-semibold'>{t('channels.codexUsage.title')}</span>
          {quotaData.plan_type && (
            <Badge
              variant='outline'
              className='h-5 border-background/20 bg-background/10 text-background uppercase hover:bg-background/15'
            >
              {quotaData.plan_type}
            </Badge>
          )}
        </div>
        {hasQuotaData ? (
          <div className='space-y-3'>
            <QuotaDetail label={t('channels.codexUsage.fiveHour')} window={fiveHourWindow} />
            <div className='border-background/20 border-t border-dashed pt-3'>
              <QuotaDetail label={t('channels.codexUsage.weekly')} window={weeklyWindow} />
            </div>
          </div>
        ) : (
          <p className='text-background/80 text-xs'>{t('channels.codexUsage.noWindowData')}</p>
        )}
        {resetCreditCount !== undefined && (
          <div className='border-background/20 space-y-1 border-t border-dashed pt-2 text-xs'>
            <div className='flex items-center justify-between gap-4'>
              <span className='text-background/80'>{t('channels.codexUsage.resetCredits')}</span>
              <span className='font-semibold tabular-nums'>
                {t('channels.codexUsage.resetCreditCount', { count: resetCreditCount })}
              </span>
            </div>
            {resetDetails.length > 0 ? (
              <div className='space-y-1 pt-0.5'>
                {resetDetails.map((reset, index) => (
                  <div key={reset.id} className='text-background/75 flex items-center justify-between gap-3 text-[11px]'>
                    <span className='min-w-0 truncate'>
                      {reset.title || t('quota.codex.resetCreditLabel', { index: index + 1 })}
                    </span>
                    <span className='shrink-0 tabular-nums'>
                      {reset.expiresAt
                        ? t('channels.codexUsage.resetCreditExpiresAt', { time: formatISOString(reset.expiresAt) })
                        : t('channels.codexUsage.resetCreditNoExpiry')}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <div className='text-background/75 text-[11px]'>
                {resetCredits?.next_expires_at
                  ? t('channels.codexUsage.resetCreditExpiry', { time: formatISOString(resetCredits.next_expires_at) })
                  : t('channels.codexUsage.resetCreditNoExpiry')}
              </div>
            )}
          </div>
        )}
        {quotaData.rate_limit_reset_credits_error && !resetCredits && (
          <div className='text-background/75 border-background/20 border-t border-dashed pt-2 text-[11px]'>
            {t('channels.codexUsage.resetCreditUnavailable')}
          </div>
        )}
        <div className='text-background/75 border-background/20 space-y-1 border-t border-dashed pt-2 text-[11px]'>
          {lastUpdated && <div>{t('channels.codexUsage.lastUpdated', { time: lastUpdated })}</div>}
          {nextCheck && <div>{t('channels.codexUsage.nextCheck', { time: nextCheck })}</div>}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}
