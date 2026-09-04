'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { IconCheck, IconFlask, IconLoader2, IconRefresh } from '@tabler/icons-react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { useChannels } from '../context/channels-context';
import { useBulkRecoverChannels, useTestChannel } from '../data/channels';
import { Channel } from '../data/schema';
import { ErrorDisplay } from '../utils/error-formatter';

type BulkTestStatus = 'idle' | 'testing' | 'success' | 'failed' | 'skipped';

interface BulkTestResult {
  channelID: string;
  channelName: string;
  modelID?: string;
  status: BulkTestStatus;
  latency?: number;
  error?: string;
  attempts?: number;
}

const MAX_CONCURRENT_TESTS = 4;
const LOOP_MIN_DELAY_MS = 5_000;
const LOOP_MAX_DELAY_MS = 10_000;

function randomLoopDelayMs() {
  return Math.floor(LOOP_MIN_DELAY_MS + Math.random() * (LOOP_MAX_DELAY_MS - LOOP_MIN_DELAY_MS + 1));
}

export function ChannelsBulkTestDialog() {
  const { t } = useTranslation();
  const { open, setOpen, selectedChannels, resetRowSelection, setSelectedChannels } = useChannels();
  const testChannel = useTestChannel({ silent: true });
  const bulkRecoverChannels = useBulkRecoverChannels();
  const [dialogContent, setDialogContent] = useState<HTMLDivElement | null>(null);
  const [results, setResults] = useState<Record<string, BulkTestResult>>({});
  const [isTesting, setIsTesting] = useState(false);
  const [loopUntilSuccess, setLoopUntilSuccess] = useState(false);
  const stopRequestedRef = useRef(false);

  const isDialogOpen = open === 'bulkTest';

  const resolveTestModel = useCallback((channel: Channel) => {
    return channel.defaultTestModel || channel.supportedModels[0] || '';
  }, []);

  const initializeResults = useCallback(() => {
    const nextResults = selectedChannels.reduce<Record<string, BulkTestResult>>((acc, channel) => {
      const modelID = resolveTestModel(channel);
      acc[channel.id] = {
        channelID: channel.id,
        channelName: channel.name,
        modelID: modelID || undefined,
        status: modelID ? 'idle' : 'skipped',
        error: modelID ? undefined : t('channels.dialogs.bulkTest.noTestModel'),
      };
      return acc;
    }, {});

    setResults(nextResults);
  }, [resolveTestModel, selectedChannels, t]);

  useEffect(() => {
    if (isDialogOpen && dialogContent) {
      initializeResults();
      setIsTesting(false);
      setLoopUntilSuccess(false);
      stopRequestedRef.current = false;
    }
  }, [dialogContent, initializeResults, isDialogOpen]);

  const resultList = useMemo(() => {
    return selectedChannels.map((channel) => {
      return (
        results[channel.id] ?? {
          channelID: channel.id,
          channelName: channel.name,
          modelID: resolveTestModel(channel) || undefined,
          status: 'idle' as const,
        }
      );
    });
  }, [resolveTestModel, results, selectedChannels]);

  const completedCount = useMemo(() => {
    return resultList.filter((result) => ['success', 'failed', 'skipped'].includes(result.status)).length;
  }, [resultList]);

  const successCount = useMemo(() => resultList.filter((result) => result.status === 'success').length, [resultList]);
  const failedCount = useMemo(() => resultList.filter((result) => result.status === 'failed').length, [resultList]);
  const skippedCount = useMemo(() => resultList.filter((result) => result.status === 'skipped').length, [resultList]);

  const recoverableChannels = useMemo(() => {
    return selectedChannels.filter((channel) => {
      const result = results[channel.id];
      return result?.status === 'success' && (channel.status === 'disabled' || channel.errorMessage != null);
    });
  }, [results, selectedChannels]);

  const failedChannels = useMemo(() => {
    return selectedChannels.filter((channel) => results[channel.id]?.status === 'failed');
  }, [results, selectedChannels]);

  const setResultStatus = useCallback(
    (channel: Channel, status: BulkTestStatus, extra?: Partial<BulkTestResult>) => {
      setResults((prev) => ({
        ...prev,
        [channel.id]: {
          ...prev[channel.id],
          channelID: channel.id,
          channelName: channel.name,
          modelID: prev[channel.id]?.modelID || resolveTestModel(channel) || undefined,
          status,
          ...extra,
        },
      }));
    },
    [resolveTestModel]
  );

  const runSingleTest = useCallback(
    async (channel: Channel, retryUntilSuccess: boolean) => {
      const modelID = resolveTestModel(channel);
      if (!modelID) {
        setResultStatus(channel, 'skipped', { error: t('channels.dialogs.bulkTest.noTestModel') });
        return;
      }

      let attempts = 0;
      while (!stopRequestedRef.current) {
        attempts += 1;
        setResultStatus(channel, 'testing', { error: undefined, latency: undefined, modelID, attempts });

        let success = false;
        let latency: number | undefined;
        let errorMessage: string | undefined;
        try {
          const result = await testChannel.mutateAsync({
            channelID: channel.id,
            modelID,
          });
          success = result.success;
          latency = result.success ? result.latency : undefined;
          errorMessage = result.success ? undefined : result.error || t('common.errors.internalServerError');
        } catch (error) {
          errorMessage = error instanceof Error ? error.message : t('common.errors.internalServerError');
        }

        if (success || !retryUntilSuccess || stopRequestedRef.current) {
          const finalStatus: BulkTestStatus = success ? 'success' : stopRequestedRef.current ? 'idle' : 'failed';
          setResultStatus(channel, finalStatus, {
            modelID,
            latency,
            error: finalStatus === 'failed' ? errorMessage : undefined,
            attempts,
          });
          return;
        }

        const delayMs = randomLoopDelayMs();
        for (let remaining = delayMs; remaining > 0; remaining -= 1000) {
          if (stopRequestedRef.current) {
            break;
          }
          await new Promise((resolve) => setTimeout(resolve, Math.min(remaining, 1000)));
        }
      }

      setResultStatus(channel, 'idle', { modelID, attempts });
    },
    [resolveTestModel, setResultStatus, t, testChannel]
  );

  const runBatch = useCallback(
    async (channels: Channel[], retryUntilSuccess: boolean) => {
      const runnableChannels = channels.filter((channel) => !!resolveTestModel(channel));
      if (runnableChannels.length === 0) {
        return;
      }

      const queue = [...runnableChannels];
      // A loop run intentionally uses one worker to avoid burst traffic while
      // each channel waits for its own randomized retry window.
      const workerCount = retryUntilSuccess ? 1 : Math.min(MAX_CONCURRENT_TESTS, queue.length);

      const workers = Array.from({ length: workerCount }, async () => {
        while (queue.length > 0) {
          const channel = queue.shift();
          if (!channel) {
            return;
          }

          await runSingleTest(channel, retryUntilSuccess);
        }
      });

      await Promise.all(workers);
    },
    [resolveTestModel, runSingleTest]
  );

  const handleRunAll = useCallback(async () => {
    if (selectedChannels.length === 0 || isTesting) {
      return;
    }

    initializeResults();
    stopRequestedRef.current = false;
    setIsTesting(true);

    try {
      await runBatch(selectedChannels, loopUntilSuccess);
    } finally {
      setIsTesting(false);
    }
  }, [initializeResults, isTesting, loopUntilSuccess, runBatch, selectedChannels]);

  const handleRetryFailed = useCallback(async () => {
    if (failedChannels.length === 0 || isTesting) {
      return;
    }

    failedChannels.forEach((channel) => {
      setResultStatus(channel, 'idle', { error: undefined, latency: undefined });
    });

    stopRequestedRef.current = false;
    setIsTesting(true);
    try {
      await runBatch(failedChannels, loopUntilSuccess);
    } finally {
      setIsTesting(false);
    }
  }, [failedChannels, isTesting, loopUntilSuccess, runBatch, setResultStatus]);

  const handleStop = useCallback(() => {
    stopRequestedRef.current = true;
  }, []);

  const handleRecoverChannels = useCallback(async () => {
    const ids = recoverableChannels.map((channel) => channel.id);
    if (ids.length === 0) {
      return;
    }

    try {
      await bulkRecoverChannels.mutateAsync(ids);
      resetRowSelection();
      setSelectedChannels([]);
      setOpen(null);
    } catch (_error) {
      // Errors are surfaced by the mutation toast.
    }
  }, [bulkRecoverChannels, recoverableChannels, resetRowSelection, setOpen, setSelectedChannels]);

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      if (!nextOpen && isTesting) {
        if (loopUntilSuccess) {
          handleStop();
        }
        return;
      }

      if (!nextOpen) {
        setOpen(null);
        return;
      }

      if (!isDialogOpen) {
        setOpen('bulkTest');
      }
    },
    [handleStop, isDialogOpen, isTesting, loopUntilSuccess, setOpen]
  );

  const getStatusBadge = useCallback(
    (status: BulkTestStatus) => {
      switch (status) {
        case 'testing':
          return <Badge variant='secondary'>{t('channels.dialogs.bulkTest.testing')}</Badge>;
        case 'success':
          return <Badge className='border-green-200 bg-green-100 text-green-800'>{t('channels.dialogs.bulkTest.success')}</Badge>;
        case 'failed':
          return <Badge variant='destructive'>{t('channels.dialogs.bulkTest.failed')}</Badge>;
        case 'skipped':
          return <Badge variant='outline'>{t('channels.dialogs.bulkTest.skipped')}</Badge>;
        default:
          return <Badge variant='outline'>{t('channels.dialogs.bulkTest.idle')}</Badge>;
      }
    },
    [t]
  );

  if (selectedChannels.length === 0 && !isDialogOpen) {
    return null;
  }

  return (
    <Dialog open={isDialogOpen} onOpenChange={handleOpenChange}>
      <DialogContent ref={setDialogContent} className='flex max-h-[90vh] flex-col sm:max-w-5xl'>
        <DialogHeader className='shrink-0'>
          <DialogTitle>{t('channels.dialogs.bulkTest.title')}</DialogTitle>
          <DialogDescription>{t('channels.dialogs.bulkTest.description', { count: selectedChannels.length })}</DialogDescription>
        </DialogHeader>

        <div className='flex min-h-0 flex-1 flex-col'>
          <div className='grid shrink-0 gap-3 border-b pb-4 md:grid-cols-4'>
            <div className='rounded-lg border bg-slate-50 p-3'>
              <div className='text-muted-foreground text-xs'>{t('channels.dialogs.bulkTest.progress', { completed: completedCount, total: selectedChannels.length })}</div>
              <div className='mt-1 text-lg font-semibold'>
                {completedCount}/{selectedChannels.length}
              </div>
            </div>
            <div className='rounded-lg border bg-green-50 p-3'>
              <div className='text-xs text-green-700'>{t('channels.dialogs.bulkTest.summary.success', { count: successCount })}</div>
              <div className='mt-1 text-lg font-semibold text-green-800'>{successCount}</div>
            </div>
            <div className='rounded-lg border bg-red-50 p-3'>
              <div className='text-xs text-red-700'>{t('channels.dialogs.bulkTest.summary.failed', { count: failedCount })}</div>
              <div className='mt-1 text-lg font-semibold text-red-800'>{failedCount}</div>
            </div>
            <div className='rounded-lg border bg-amber-50 p-3'>
              <div className='text-xs text-amber-700'>{t('channels.dialogs.bulkTest.summary.skipped', { count: skippedCount })}</div>
              <div className='mt-1 text-lg font-semibold text-amber-800'>{skippedCount}</div>
            </div>
          </div>

          <div className='bg-muted/20 mt-4 flex items-center justify-between gap-4 rounded-lg border px-4 py-3'>
            <div className='min-w-0'>
              <div className='flex items-center gap-2 text-sm font-medium'>
                {t('channels.dialogs.bulkTest.loopLabel')}
                <Badge variant='outline' className='text-[11px] font-normal'>
                  {t('channels.dialogs.bulkTest.loopInterval')}
                </Badge>
              </div>
              <p className='text-muted-foreground mt-1 text-xs'>{t('channels.dialogs.bulkTest.loopDescription')}</p>
            </div>
            <Switch
              checked={loopUntilSuccess}
              onCheckedChange={setLoopUntilSuccess}
              disabled={isTesting || bulkRecoverChannels.isPending}
              aria-label={t('channels.dialogs.bulkTest.loopLabel')}
            />
          </div>

          <div className='min-h-0 flex-1 overflow-y-auto py-4'>
            <div className='overflow-x-auto rounded-lg border'>
              <Table className='w-full table-fixed'>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-[24%]'>{t('channels.dialogs.bulkTest.channelColumn')}</TableHead>
                    <TableHead className='w-[12%]'>{t('channels.dialogs.bulkTest.currentStatusColumn')}</TableHead>
                    <TableHead className='w-[24%]'>{t('channels.dialogs.bulkTest.testModelColumn')}</TableHead>
                    <TableHead className='w-[16%]'>{t('channels.dialogs.bulkTest.resultColumn')}</TableHead>
                    <TableHead className='w-[24%]'>{t('channels.dialogs.bulkTest.detailsColumn')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {selectedChannels.map((channel) => {
                    const result = results[channel.id];
                    return (
                      <TableRow key={channel.id}>
                        <TableCell className='align-top'>
                          <div className='truncate font-medium' title={channel.name}>
                            {channel.name}
                          </div>
                        </TableCell>
                        <TableCell className='align-top'>
                          <div className='truncate'>{t(`channels.status.${channel.status}`)}</div>
                        </TableCell>
                        <TableCell className='align-top'>
                          <div className='truncate' title={result?.modelID || '-'}>
                            {result?.modelID || '-'}
                          </div>
                        </TableCell>
                        <TableCell className='align-top'>
                          <div className='space-y-1'>
                            {getStatusBadge(result?.status || 'idle')}
                            {typeof result?.latency === 'number' && <div className='text-muted-foreground text-xs'>{result.latency.toFixed(2)}s</div>}
                          </div>
                        </TableCell>
                        <TableCell className='align-top'>
                          {result?.status === 'testing' ? (
                            <div className='text-muted-foreground flex max-w-full items-center gap-2 text-sm'>
                              <IconLoader2 className='h-4 w-4 animate-spin' />
                              <span className='truncate'>{t('channels.dialogs.bulkTest.testing')}</span>
                            </div>
                          ) : result?.error ? (
                            <div className='max-w-full overflow-hidden'>
                              <ErrorDisplay error={result.error} messageClassName='block max-w-full break-all text-xs font-medium text-red-600 whitespace-pre-wrap' />
                            </div>
                          ) : result?.status === 'success' ? (
                            <span className='block truncate text-xs text-green-700'>{t('channels.dialogs.bulkTest.success')}</span>
                          ) : (
                            <span className='text-muted-foreground block truncate text-xs'>-</span>
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          </div>
        </div>

        <DialogFooter className='shrink-0 !flex-col gap-2 border-t pt-4 sm:!flex-row sm:items-center sm:justify-between'>
          <div className='text-muted-foreground min-w-0 text-sm'>
            {recoverableChannels.length > 0
              ? t('channels.dialogs.bulkTest.recoverButton', { count: recoverableChannels.length })
              : t('channels.dialogs.bulkTest.noRecoverableChannels')}
          </div>
          <div className='flex flex-wrap justify-end gap-2'>
            <Button
              variant='outline'
              onClick={() => (isTesting && loopUntilSuccess ? handleStop() : handleOpenChange(false))}
              disabled={(!isTesting && bulkRecoverChannels.isPending) || (isTesting && !loopUntilSuccess)}
            >
              {isTesting && loopUntilSuccess ? t('channels.dialogs.bulkTest.stopButton') : t('common.buttons.cancel')}
            </Button>
            <Button variant='outline' onClick={handleRetryFailed} disabled={failedChannels.length === 0 || isTesting || bulkRecoverChannels.isPending}>
              <IconRefresh className='mr-2 h-4 w-4' />
              {t('channels.dialogs.bulkTest.retryFailedButton', { count: failedChannels.length })}
            </Button>
            <Button variant='outline' onClick={handleRunAll} disabled={isTesting || bulkRecoverChannels.isPending || selectedChannels.length === 0}>
              {isTesting ? <IconLoader2 className='mr-2 h-4 w-4 animate-spin' /> : <IconFlask className='mr-2 h-4 w-4' />}
              {completedCount > 0 ? t('channels.dialogs.bulkTest.runAgainButton') : t('channels.dialogs.bulkTest.runButton', { count: selectedChannels.length })}
            </Button>
            <Button onClick={handleRecoverChannels} disabled={recoverableChannels.length === 0 || isTesting || bulkRecoverChannels.isPending}>
              {bulkRecoverChannels.isPending ? <IconLoader2 className='mr-2 h-4 w-4 animate-spin' /> : <IconCheck className='mr-2 h-4 w-4' />}
              {t('channels.dialogs.bulkTest.recoverButton', { count: recoverableChannels.length })}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
