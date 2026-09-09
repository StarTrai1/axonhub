import { useEffect, useMemo, useState } from 'react';
import { format } from 'date-fns';
import { IconCalendarClock, IconPlus, IconTrash } from '@tabler/icons-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { Channel } from '../data/schema';
import { useChannelHealthCheckSchedules, useUpdateChannelHealthCheckSchedules } from '../data/channels';

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentRow: Channel;
}

const timePattern = /^(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d$/;

function formatScheduleTimestamp(value?: string | null) {
  if (!value || value.startsWith('0001-')) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : format(date, 'yyyy-MM-dd HH:mm:ss');
}

export function ChannelsScheduledHealthCheckDialog({ open, onOpenChange, currentRow }: Props) {
  const { t } = useTranslation();
  const { data, isLoading } = useChannelHealthCheckSchedules(currentRow.id, open);
  const updateSchedules = useUpdateChannelHealthCheckSchedules();
  const [times, setTimes] = useState<string[]>([]);
  const savedTimes = data ? JSON.stringify(data.times) : undefined;
  const runtime = data?.runtime;
  const lastRun = runtime?.lastRun;

  useEffect(() => {
    if (open && savedTimes) {
      setTimes(JSON.parse(savedTimes));
    }
  }, [savedTimes, open]);

  const hasInvalidTime = useMemo(() => times.some((value) => !timePattern.test(value)), [times]);
  const hasDuplicateTime = useMemo(() => new Set(times).size !== times.length, [times]);

  const addTime = () => {
    const used = new Set(times);
    const value = Array.from({ length: 24 }, (_, hour) => `${String(hour).padStart(2, '0')}:00:00`).find(
      (candidate) => !used.has(candidate)
    );
    setTimes((current) => [...current, value ?? '00:00:00']);
  };

  const save = async () => {
    if (hasInvalidTime || hasDuplicateTime) {
      toast.error(
        t(hasInvalidTime ? 'channels.dialogs.scheduledHealthCheck.invalidTime' : 'channels.dialogs.scheduledHealthCheck.duplicateTime')
      );
      return;
    }

    try {
      await updateSchedules.mutateAsync({ channelID: currentRow.id, times });
      toast.success(t('channels.dialogs.scheduledHealthCheck.saved'));
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('common.errors.internalServerError'));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-[560px]'>
        <DialogHeader className='text-left'>
          <DialogTitle className='flex items-center gap-2'>
            <IconCalendarClock className='h-5 w-5' />
            {t('channels.dialogs.scheduledHealthCheck.title')}
          </DialogTitle>
          <DialogDescription>
            {t('channels.dialogs.scheduledHealthCheck.description', { name: currentRow.name })}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='bg-muted/35 flex items-start justify-between gap-4 rounded-lg border px-4 py-3'>
            <div className='min-w-0 space-y-1'>
              <p className='text-sm font-medium'>{t('channels.dialogs.scheduledHealthCheck.serverTime')}</p>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {t('channels.dialogs.scheduledHealthCheck.serverTimeHint', { timezone: data?.timezone ?? '—' })}
              </p>
            </div>
            <Badge variant='outline' className='shrink-0'>
              {t('channels.dialogs.scheduledHealthCheck.daily')}
            </Badge>
          </div>

          <Separator />

          <div className='space-y-2 rounded-lg border px-4 py-3'>
            <p className='text-sm font-medium'>{t('channels.dialogs.scheduledHealthCheck.executionStatus')}</p>
            <dl className='grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs'>
              <dt className='text-muted-foreground'>{t('channels.dialogs.scheduledHealthCheck.nextRun')}</dt>
              <dd className='min-w-0 break-words tabular-nums'>{formatScheduleTimestamp(runtime?.nextRunAt)}</dd>
              <dt className='text-muted-foreground'>{t('channels.dialogs.scheduledHealthCheck.lastDispatch')}</dt>
              <dd className='min-w-0 break-words tabular-nums'>{formatScheduleTimestamp(runtime?.lastDispatchAt)}</dd>
              <dt className='text-muted-foreground'>{t('channels.dialogs.scheduledHealthCheck.scheduledFor')}</dt>
              <dd className='min-w-0 break-words tabular-nums'>{formatScheduleTimestamp(lastRun?.scheduledFor)}</dd>
              <dt className='text-muted-foreground'>{t('channels.dialogs.scheduledHealthCheck.actualStart')}</dt>
              <dd className='min-w-0 break-words tabular-nums'>{formatScheduleTimestamp(lastRun?.startedAt)}</dd>
              <dt className='text-muted-foreground'>{t('channels.dialogs.scheduledHealthCheck.result')}</dt>
              <dd className='min-w-0 break-words'>
                {t(`channels.dialogs.scheduledHealthCheck.states.${lastRun?.status || 'never'}`)}
                {lastRun?.modelID && ` · ${lastRun.modelID}`}
              </dd>
            </dl>
            {(lastRun?.error || runtime?.checkpointError) && (
              <p className='text-destructive break-words text-xs'>{runtime?.checkpointError || lastRun?.error}</p>
            )}
            <p className='text-muted-foreground text-xs leading-relaxed'>
              {t('channels.dialogs.scheduledHealthCheck.executionHint')}
            </p>
          </div>

          <div className='flex items-center justify-between gap-3'>
            <div>
              <p className='text-sm font-medium'>{t('channels.dialogs.scheduledHealthCheck.times')}</p>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t('channels.dialogs.scheduledHealthCheck.proxyHint', { model: currentRow.defaultTestModel })}
              </p>
            </div>
            <Button type='button' variant='outline' size='sm' onClick={addTime} disabled={isLoading}>
              <IconPlus className='mr-1.5 h-4 w-4' />
              {t('channels.dialogs.scheduledHealthCheck.add')}
            </Button>
          </div>

          {isLoading ? (
            <div className='text-muted-foreground rounded-lg border border-dashed px-4 py-8 text-center text-sm'>
              {t('common.loading')}
            </div>
          ) : times.length === 0 ? (
            <button
              type='button'
              onClick={addTime}
              className='text-muted-foreground hover:border-primary/50 hover:bg-muted/30 w-full rounded-lg border border-dashed px-4 py-8 text-center text-sm transition-colors'
            >
              {t('channels.dialogs.scheduledHealthCheck.empty')}
            </button>
          ) : (
            <div className='max-h-[300px] space-y-2 overflow-y-auto pr-1'>
              {times.map((value, index) => (
                <div key={index} className='bg-card flex items-center gap-3 rounded-lg border p-2.5'>
                  <span className='text-muted-foreground w-8 text-center text-xs tabular-nums'>{index + 1}</span>
                  <Input
                    type='time'
                    step={1}
                    value={value}
                    onChange={(event) => {
                      setTimes((current) => current.map((item, itemIndex) => (itemIndex === index ? event.target.value : item)));
                    }}
                    className='h-9 flex-1 font-mono tabular-nums'
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='text-muted-foreground hover:text-destructive h-9 w-9 shrink-0'
                    onClick={() => setTimes((current) => current.filter((_, itemIndex) => itemIndex !== index))}
                    aria-label={t('common.buttons.delete')}
                  >
                    <IconTrash className='h-4 w-4' />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('common.buttons.cancel')}
          </Button>
          <Button onClick={save} disabled={isLoading || updateSchedules.isPending}>
            {t('common.buttons.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
