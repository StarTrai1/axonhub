'use client';

import React, { useEffect, useMemo, useState } from 'react';
import { Activity, Clock3, Database, FileJson, Gauge, Loader2, Play, Save, Search, ShieldAlert, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { useSystemContext } from '../context/system-context';
import {
  CleanupOption,
  StorageCleanupPreviewItem,
  StorageCleanupResourceType,
  usePreviewStorageCleanup,
  useStartStorageCleanup,
  useStorageCleanupJob,
  useStoragePolicy,
  useUpdateStoragePolicy,
} from '../data/system';

const cleanupResources: StorageCleanupResourceType[] = [
  'request_payloads',
  'response_payloads',
  'requests',
  'usage_logs',
  'channel_probes',
];

const sensitiveResources = new Set<StorageCleanupResourceType>([
  'request_payloads',
  'response_payloads',
  'requests',
  'usage_logs',
]);

const resourceIcons: Record<StorageCleanupResourceType, React.ComponentType<{ className?: string }>> = {
  request_payloads: FileJson,
  response_payloads: Database,
  requests: Trash2,
  usage_logs: Gauge,
  channel_probes: Activity,
};

type ManualSelectionState = Record<StorageCleanupResourceType, { selected: boolean; retentionDays: number }>;

const defaultManualSelection: ManualSelectionState = {
  request_payloads: { selected: true, retentionDays: 0 },
  response_payloads: { selected: false, retentionDays: 7 },
  requests: { selected: false, retentionDays: 30 },
  usage_logs: { selected: false, retentionDays: 30 },
  channel_probes: { selected: false, retentionDays: 3 },
};

function formatBytes(bytes: number) {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function StoragePolicySettings() {
  const { t } = useTranslation();
  const { isLoading, setIsLoading } = useSystemContext();
  const { data: storagePolicy, isLoading: isLoadingStoragePolicy } = useStoragePolicy();
  const updateStoragePolicy = useUpdateStoragePolicy();
  const { mutateAsync: previewCleanupAsync, isPending: isPreviewPending } = usePreviewStorageCleanup();
  const startCleanup = useStartStorageCleanup();
  const { data: cleanupJob } = useStorageCleanupJob();

  const [storagePolicyState, setStoragePolicyState] = useState({
    storeChunks: storagePolicy?.storeChunks ?? false,
    livePreview: storagePolicy?.livePreview ?? false,
    storeRequestBody: storagePolicy?.storeRequestBody ?? true,
    storeResponseBody: storagePolicy?.storeResponseBody ?? true,
    cleanupOptions: storagePolicy?.cleanupOptions ?? [],
  });
  const [manualDialogOpen, setManualDialogOpen] = useState(false);
  const [manualConfirmOpen, setManualConfirmOpen] = useState(false);
  const [manualSelection, setManualSelection] = useState<ManualSelectionState>(defaultManualSelection);
  const [previewItems, setPreviewItems] = useState<StorageCleanupPreviewItem[]>([]);
  const [confirmationText, setConfirmationText] = useState('');
  const [autoConfirmOpen, setAutoConfirmOpen] = useState(false);
  const [autoConfirmationText, setAutoConfirmationText] = useState('');

  useEffect(() => {
    if (storagePolicy) {
      setStoragePolicyState({
        storeChunks: storagePolicy.storeChunks,
        livePreview: storagePolicy.livePreview,
        storeRequestBody: storagePolicy.storeRequestBody,
        storeResponseBody: storagePolicy.storeResponseBody,
        cleanupOptions: storagePolicy.cleanupOptions,
      });
    }
  }, [storagePolicy]);

  const selectedResources = useMemo(
    () =>
      cleanupResources
        .filter((resourceType) => manualSelection[resourceType].selected)
        .map((resourceType) => ({
          resourceType,
          retentionDays: manualSelection[resourceType].retentionDays,
        })),
    [manualSelection]
  );

  const selectedSensitive = selectedResources.some((selection) => sensitiveResources.has(selection.resourceType));
  const previewIsCurrent =
    previewItems.length === selectedResources.length &&
    selectedResources.every((selection) =>
      previewItems.some(
        (item) => item.resourceType === selection.resourceType && item.retentionDays === selection.retentionDays
      )
    );

  useEffect(() => {
    if (!manualDialogOpen || selectedResources.length === 0) {
      setPreviewItems([]);
      return;
    }

    const timer = setTimeout(() => {
      previewCleanupAsync({ resources: selectedResources })
        .then(setPreviewItems)
        .catch(() => setPreviewItems([]));
    }, 400);

    return () => clearTimeout(timer);
  }, [manualDialogOpen, selectedResources, previewCleanupAsync]);

  const handleCleanupOptionChange = (index: number, field: keyof CleanupOption, value: boolean | number) => {
    const cleanupOptions = [...storagePolicyState.cleanupOptions];
    cleanupOptions[index] = { ...cleanupOptions[index], [field]: value };
    setStoragePolicyState({ ...storagePolicyState, cleanupOptions });
  };

  const persistStoragePolicy = async () => {
    setIsLoading(true);
    try {
      await updateStoragePolicy.mutateAsync({
        storeChunks: storagePolicyState.storeChunks,
        livePreview: storagePolicyState.livePreview,
        storeRequestBody: storagePolicyState.storeRequestBody,
        storeResponseBody: storagePolicyState.storeResponseBody,
        cleanupOptions: storagePolicyState.cleanupOptions.map((option) => ({
          resourceType: option.resourceType,
          enabled: option.enabled,
          cleanupDays: option.cleanupDays,
        })),
      });
      setAutoConfirmOpen(false);
      setAutoConfirmationText('');
    } finally {
      setIsLoading(false);
    }
  };

  const handleSave = () => {
    const makesSensitiveCleanupMoreAggressive = storagePolicyState.cleanupOptions.some((option) => {
      if (!sensitiveResources.has(option.resourceType as StorageCleanupResourceType) || !option.enabled) return false;
      const saved = storagePolicy?.cleanupOptions.find((item) => item.resourceType === option.resourceType);
      return !saved?.enabled || option.cleanupDays < saved.cleanupDays;
    });

    if (makesSensitiveCleanupMoreAggressive) {
      setAutoConfirmOpen(true);
      return;
    }
    void persistStoragePolicy();
  };

  const handleManualDialogOpen = (open: boolean) => {
    setManualDialogOpen(open);
    if (!open) return;

    const next = structuredClone(defaultManualSelection);
    for (const resourceType of cleanupResources) {
      const option = storagePolicyState.cleanupOptions.find((item) => item.resourceType === resourceType);
      if (option && resourceType !== 'request_payloads') {
        next[resourceType].retentionDays = option.cleanupDays;
      }
    }
    setManualSelection(next);
    setPreviewItems([]);
  };

  const updateManualSelection = (
    resourceType: StorageCleanupResourceType,
    patch: Partial<ManualSelectionState[StorageCleanupResourceType]>
  ) => {
    setManualSelection((current) => ({
      ...current,
      [resourceType]: { ...current[resourceType], ...patch },
    }));
  };

  const openManualConfirmation = () => {
    if (selectedResources.length === 0 || isPreviewPending || !previewIsCurrent) return;
    setManualDialogOpen(false);
    setConfirmationText('');
    setManualConfirmOpen(true);
  };

  const runManualCleanup = () => {
    startCleanup.mutate({
      resources: selectedResources,
      confirmation: selectedSensitive ? confirmationText : undefined,
    });
    setManualConfirmOpen(false);
  };

  const hasChanges =
    !!storagePolicy &&
    (storagePolicy.storeChunks !== storagePolicyState.storeChunks ||
      storagePolicy.livePreview !== storagePolicyState.livePreview ||
      storagePolicy.storeRequestBody !== storagePolicyState.storeRequestBody ||
      storagePolicy.storeResponseBody !== storagePolicyState.storeResponseBody ||
      JSON.stringify(storagePolicy.cleanupOptions) !== JSON.stringify(storagePolicyState.cleanupOptions));

  if (isLoadingStoragePolicy) {
    return (
      <div className='flex h-32 items-center justify-center'>
        <Loader2 className='h-6 w-6 animate-spin' />
        <span className='text-muted-foreground ml-2'>{t('common.loading')}</span>
      </div>
    );
  }

  const cleanupRunning = cleanupJob?.status === 'running';
  const cleanupPhase = cleanupJob?.phase
    ? t(`system.storage.policy.job.phases.${cleanupJob.phase}`, {
        defaultValue: t(`system.storage.policy.resourceTypes.${cleanupJob.phase}`, { defaultValue: cleanupJob.phase }),
      })
    : '';

  return (
    <>
      <Card>
        <CardHeader className='gap-4 sm:flex-row sm:items-start sm:justify-between'>
          <div className='space-y-1.5'>
            <CardTitle>{t('system.storage.policy.title')}</CardTitle>
            <CardDescription>{t('system.storage.policy.description')}</CardDescription>
          </div>
          <Button variant='outline' size='sm' onClick={() => handleManualDialogOpen(true)} disabled={cleanupRunning || isLoading}>
            {cleanupRunning ? <Loader2 className='mr-2 h-4 w-4 animate-spin' /> : <Play className='mr-2 h-4 w-4' />}
            {cleanupRunning ? t('system.storage.policy.cleanupRunning') : t('system.storage.policy.runCleanupNow')}
          </Button>
        </CardHeader>
        <CardContent className='space-y-7'>
          {cleanupJob && (
            <Alert variant={cleanupJob.status === 'failed' ? 'destructive' : 'default'}>
              {cleanupRunning ? <Loader2 className='animate-spin' /> : <Clock3 />}
              <AlertTitle>{t(`system.storage.policy.job.status.${cleanupJob.status}`)}</AlertTitle>
              <AlertDescription>
                {cleanupJob.status === 'failed'
                  ? cleanupJob.error
                  : t('system.storage.policy.job.phase', { phase: cleanupPhase })}
              </AlertDescription>
            </Alert>
          )}

          <div className='grid gap-5 md:grid-cols-2'>
            {([
              ['storeChunks', 'storage-policy-store-chunks'],
              ['livePreview', 'storage-policy-live-preview'],
              ['storeRequestBody', 'storage-policy-store-request-body'],
              ['storeResponseBody', 'storage-policy-store-response-body'],
            ] as const).map(([key, id]) => (
              <div key={key} className='flex items-start justify-between gap-4 rounded-lg border p-4'>
                <div className='space-y-1'>
                  <Label htmlFor={id}>{t(`system.storage.policy.${key}.label`)}</Label>
                  <p className='text-muted-foreground text-sm'>{t(`system.storage.policy.${key}.description`)}</p>
                </div>
                <Switch
                  id={id}
                  className='mt-0.5 shrink-0'
                  checked={storagePolicyState[key]}
                  onCheckedChange={(checked) => setStoragePolicyState({ ...storagePolicyState, [key]: checked })}
                  disabled={isLoading}
                />
              </div>
            ))}
          </div>

          <div className='space-y-4'>
            <div className='space-y-1'>
              <div className='text-lg font-medium'>{t('system.storage.policy.cleanupOptions')}</div>
              <p className='text-muted-foreground text-sm'>{t('system.storage.policy.cleanupDescription')}</p>
            </div>
            <div className='grid gap-3 lg:grid-cols-2'>
              {storagePolicyState.cleanupOptions.map((option, index) => {
                const resourceType = option.resourceType as StorageCleanupResourceType;
                const Icon = resourceIcons[resourceType] ?? Database;
                return (
                  <div key={option.resourceType} className='rounded-lg border p-4' id={`storage-cleanup-option-${option.resourceType}`}>
                    <div className='flex items-start justify-between gap-4'>
                      <div className='flex min-w-0 gap-3'>
                        <div className='bg-muted flex h-9 w-9 shrink-0 items-center justify-center rounded-md'>
                          <Icon className='h-4 w-4' />
                        </div>
                        <div className='min-w-0 space-y-1'>
                          <div className='flex flex-wrap items-center gap-2 font-medium'>
                            {t(`system.storage.policy.resourceTypes.${option.resourceType}`, { defaultValue: option.resourceType })}
                            {resourceType === 'request_payloads' && (
                              <Badge variant='secondary'>{t('system.storage.policy.recommended')}</Badge>
                            )}
                            {sensitiveResources.has(resourceType) && (
                              <Badge variant='outline'>{t('system.storage.policy.sensitive')}</Badge>
                            )}
                          </div>
                          <p className='text-muted-foreground text-sm'>
                            {t(`system.storage.policy.resourceDescriptions.${option.resourceType}`, { defaultValue: '' })}
                          </p>
                        </div>
                      </div>
                      <Switch
                        checked={option.enabled}
                        onCheckedChange={(checked) => handleCleanupOptionChange(index, 'enabled', checked)}
                        disabled={isLoading}
                      />
                    </div>
                    {option.enabled && (
                      <div className='bg-muted/40 mt-4 flex items-center gap-2 rounded-md px-3 py-2'>
                        <Label htmlFor={`cleanup-days-${index}`} className='text-sm'>
                          {t('system.storage.policy.cleanupDays')}
                        </Label>
                        <Input
                          id={`cleanup-days-${index}`}
                          type='number'
                          min='1'
                          max='3650'
                          value={option.cleanupDays}
                          onChange={(event) =>
                            handleCleanupOptionChange(index, 'cleanupDays', Math.max(1, Math.min(3650, Number(event.target.value) || 1)))
                          }
                          className='ml-auto w-24'
                          disabled={isLoading}
                        />
                        <span className='text-muted-foreground text-sm'>{t('system.storage.policy.days')}</span>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>

          <div className='flex justify-end'>
            <Button onClick={handleSave} disabled={isLoading || updateStoragePolicy.isPending || !hasChanges} size='sm'>
              {isLoading || updateStoragePolicy.isPending ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : (
                <Save className='mr-2 h-4 w-4' />
              )}
              {t(isLoading || updateStoragePolicy.isPending ? 'system.buttons.saving' : 'system.buttons.save')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Dialog open={manualDialogOpen} onOpenChange={handleManualDialogOpen}>
        <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>{t('system.storage.policy.runCleanupManualTitle')}</DialogTitle>
            <DialogDescription>{t('system.storage.policy.runCleanupManualDescription')}</DialogDescription>
          </DialogHeader>
          <Alert variant='destructive'>
            <ShieldAlert />
            <AlertTitle>{t('system.storage.policy.manualWarningTitle')}</AlertTitle>
            <AlertDescription>{t('system.storage.policy.manualWarningDescription')}</AlertDescription>
          </Alert>
          <div className='space-y-3'>
            {cleanupResources.map((resourceType) => {
              const Icon = resourceIcons[resourceType];
              const state = manualSelection[resourceType];
              const preview = previewItems.find((item) => item.resourceType === resourceType);
              return (
                <div key={resourceType} className='rounded-lg border p-3'>
                  <div className='flex items-start gap-3'>
                    <Checkbox
                      id={`manual-cleanup-${resourceType}`}
                      checked={state.selected}
                      onCheckedChange={(checked) => updateManualSelection(resourceType, { selected: checked === true })}
                      className='mt-1'
                    />
                    <Icon className='text-muted-foreground mt-0.5 h-5 w-5 shrink-0' />
                    <div className='min-w-0 flex-1'>
                      <Label htmlFor={`manual-cleanup-${resourceType}`} className='cursor-pointer font-medium'>
                        {t(`system.storage.policy.resourceTypes.${resourceType}`)}
                      </Label>
                      <p className='text-muted-foreground mt-0.5 text-sm'>
                        {t(`system.storage.policy.resourceDescriptions.${resourceType}`)}
                      </p>
                      {state.selected && (
                        <div className='mt-3 flex flex-wrap items-center gap-2'>
                          <span className='text-sm'>{t('system.storage.policy.runCleanupRetentionLabel')}</span>
                          <Input
                            type='number'
                            min='0'
                            max='3650'
                            value={state.retentionDays}
                            onChange={(event) =>
                              updateManualSelection(resourceType, {
                                retentionDays: Math.max(0, Math.min(3650, Number(event.target.value) || 0)),
                              })
                            }
                            className='w-24'
                          />
                          <span className='text-muted-foreground text-sm'>{t('system.storage.policy.days')}</span>
                          {state.retentionDays === 0 && (
                            <Badge variant='destructive'>{t('system.storage.policy.allFinishedData')}</Badge>
                          )}
                          <span className='text-muted-foreground ml-auto text-sm'>
                            {isPreviewPending
                              ? t('system.storage.policy.runCleanupPreviewLoading')
                              : t('system.storage.policy.previewSummary', {
                                  count: preview?.estimatedCount ?? 0,
                                  size: formatBytes(preview?.estimatedBytes ?? 0),
                                })}
                          </span>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setManualDialogOpen(false)}>
              {t('system.storage.policy.runCleanupCancel')}
            </Button>
            <Button
              onClick={openManualConfirmation}
              disabled={selectedResources.length === 0 || isPreviewPending || !previewIsCurrent}
            >
              <Search className='mr-2 h-4 w-4' />
              {t('system.storage.policy.reviewCleanup')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={manualConfirmOpen} onOpenChange={setManualConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('system.storage.policy.runCleanupConfirmTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('system.storage.policy.runCleanupConfirmDescription')}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className='space-y-3'>
            {previewItems.filter((item) => selectedResources.some((resource) => resource.resourceType === item.resourceType)).map((item) => (
              <div key={item.resourceType} className='flex items-center justify-between rounded-md border px-3 py-2 text-sm'>
                <span>{t(`system.storage.policy.resourceTypes.${item.resourceType}`)}</span>
                <span className='text-muted-foreground'>
                  {t('system.storage.policy.previewSummary', {
                    count: item.estimatedCount,
                    size: formatBytes(item.estimatedBytes),
                  })}
                </span>
              </div>
            ))}
            {selectedSensitive && (
              <div className='space-y-2'>
                <Label htmlFor='manual-cleanup-confirmation'>{t('system.storage.policy.typeDeleteLabel')}</Label>
                <Input
                  id='manual-cleanup-confirmation'
                  value={confirmationText}
                  onChange={(event) => setConfirmationText(event.target.value)}
                  placeholder='DELETE'
                  autoComplete='off'
                />
              </div>
            )}
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('system.storage.policy.runCleanupCancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={runManualCleanup}
              disabled={startCleanup.isPending || (selectedSensitive && confirmationText !== 'DELETE')}
              className='bg-destructive text-white hover:bg-destructive/90'
            >
              {startCleanup.isPending && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {t('system.storage.policy.runCleanupConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={autoConfirmOpen} onOpenChange={setAutoConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('system.storage.policy.autoConfirmTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('system.storage.policy.autoConfirmDescription')}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className='space-y-2'>
            <Label htmlFor='auto-cleanup-confirmation'>{t('system.storage.policy.typeEnableLabel')}</Label>
            <Input
              id='auto-cleanup-confirmation'
              value={autoConfirmationText}
              onChange={(event) => setAutoConfirmationText(event.target.value)}
              placeholder='ENABLE'
              autoComplete='off'
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('system.storage.policy.runCleanupCancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void persistStoragePolicy()} disabled={autoConfirmationText !== 'ENABLE'}>
              {t('system.storage.policy.enableAutoCleanup')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
