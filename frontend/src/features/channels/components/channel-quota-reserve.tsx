import { Info } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import type { ChannelSettings } from '../data/schema';

export type QuotaReserve = NonNullable<ChannelSettings['quotaReserve']>;
export const defaultQuotaReserve: QuotaReserve = { fiveHourPercent: 90, sevenDayPercent: 90, allowSearch: false, allowImage: false, allowMemory: false };

export function ChannelQuotaReserveFields({ value, onChange }: { value: QuotaReserve | null; onChange: (value: QuotaReserve | null) => void }) {
  const { t } = useTranslation();
  const key = 'channels.dialogs.fields.quotaReserve';
  const retained = value && (value.allowSearch || value.allowImage || value.allowMemory);
  return (
    <div className='space-y-3 border-t pt-4' data-testid='channel-quota-reserve'>
      <div className='flex flex-wrap items-center gap-2'>
        <Label htmlFor='quota-reserve-enabled' className='text-sm font-medium'>{t(`${key}.label`)}</Label>
        <Tooltip>
          <TooltipTrigger asChild>
            <button type='button' className='text-muted-foreground hover:text-foreground inline-flex items-center' aria-label={t(`${key}.description`)} data-testid='quota-reserve-tip'>
              <Info className='size-3.5' />
            </button>
          </TooltipTrigger>
          <TooltipContent className='max-w-72 leading-relaxed text-wrap'><p>{t(`${key}.description`)}</p></TooltipContent>
        </Tooltip>
        <Switch id='quota-reserve-enabled' checked={value !== null} onCheckedChange={(enabled) => onChange(enabled ? { ...defaultQuotaReserve } : null)} data-testid='quota-reserve-enabled' />
      </div>
      {value && (
        <div className='space-y-3'>
          <div className='grid grid-cols-2 gap-3'>
            {(['fiveHourPercent', 'sevenDayPercent'] as const).map((field) => (
              <div className='min-w-0 space-y-1.5' key={field}>
                <Label htmlFor={`quota-reserve-${field}`} className='text-sm'>{t(`${key}.${field}`)}</Label>
                <div className='relative'>
                  <Input id={`quota-reserve-${field}`} data-testid={`quota-reserve-${field}`} type='number' min={1} max={100} step={1} inputMode='numeric' value={value[field] ?? ''} placeholder={t(`${key}.unlimited`)} onChange={(event) => onChange({ ...value, [field]: event.target.value === '' ? null : Number(event.target.value) })} className='pr-8' />
                  <span className='text-muted-foreground pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-sm'>%</span>
                </div>
              </div>
            ))}
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='quota-reserve-action' className='text-sm'>{t(`${key}.action`)}</Label>
            <Select value={retained ? 'tools' : 'pause'} onValueChange={(mode) => onChange({ ...value, allowSearch: mode === 'tools', allowImage: mode === 'tools', allowMemory: mode === 'tools' })}>
              <SelectTrigger id='quota-reserve-action' data-testid='quota-reserve-action'><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value='pause'>{t(`${key}.pause`)}</SelectItem><SelectItem value='tools'>{t(`${key}.tools`)}</SelectItem></SelectContent>
            </Select>
          </div>
          {retained && <div className='flex flex-wrap gap-x-5 gap-y-3 pt-1'>
            {(['allowSearch', 'allowImage', 'allowMemory'] as const).map((field) => (
              <label key={field} className='flex cursor-pointer items-center gap-2 text-sm'>
                <Checkbox checked={value[field]} onCheckedChange={(checked) => onChange({ ...value, [field]: checked === true })} data-testid={`quota-reserve-${field}`} />
                {t(`${key}.${field}`)}
              </label>
            ))}
          </div>}
        </div>
      )}
    </div>
  );
}
