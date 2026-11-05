import { useTranslation } from 'react-i18next';
import { Info } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { codexSearchModelOptions } from '../utils/codex-search';

type Props = {
  searchOnly: boolean;
  imageOnly: boolean;
  searchModel: string;
  imageModel: string;
  models: string[];
  onSearchOnlyChange: (value: boolean) => void;
  onImageOnlyChange: (value: boolean) => void;
  onSearchModelChange: (value: string) => void;
  onImageModelChange: (value: string) => void;
};

export function CodexToolSettings(props: Props) {
  const { t } = useTranslation();
  const fields = [
    { id: 'search', enabled: props.searchOnly, model: props.searchModel, label: 'codexSearchOnly', modelLabel: 'codexSearchModel', onToggle: props.onSearchOnlyChange, onModel: props.onSearchModelChange, options: codexSearchModelOptions(props.models, props.searchModel) },
    { id: 'image', enabled: props.imageOnly, model: props.imageModel, label: 'codexImageOnly', modelLabel: 'codexImageModel', onToggle: props.onImageOnlyChange, onModel: props.onImageModelChange, options: [...new Set([...props.models, props.imageModel])].filter(Boolean) },
  ];
  return (
    <div className='space-y-3' data-testid='codex-tool-settings'>
      {fields.map((field) => (
        <div key={field.id} className='flex min-w-0 flex-wrap items-center gap-x-5 gap-y-2' data-testid={`codex-${field.id}-only-setting`}>
          <div className='flex min-h-8 shrink-0 items-center gap-2'>
            <label htmlFor={`codex-${field.id}-only`} className='text-sm font-medium' data-testid={`codex-${field.id}-only-label`}>
              {t(`channels.dialogs.fields.${field.label}.label`)}
            </label>
            <Tooltip>
              <TooltipTrigger asChild>
                <button type='button' className='text-muted-foreground hover:text-foreground inline-flex items-center' aria-label={t(`channels.dialogs.fields.${field.label}.description`)} data-testid={`codex-${field.id}-only-tip`}>
                  <Info className='size-3.5' />
                </button>
              </TooltipTrigger>
              <TooltipContent className='max-w-72 leading-relaxed text-wrap'>
                {t(`channels.dialogs.fields.${field.label}.description`)}
              </TooltipContent>
            </Tooltip>
            <Switch id={`codex-${field.id}-only`} checked={field.enabled} onCheckedChange={field.onToggle} data-testid={`codex-${field.id}-only-toggle`} />
          </div>
          {field.enabled && (
            <div className='flex min-w-0 max-w-full flex-wrap items-center gap-x-2 gap-y-1.5'>
              <label htmlFor={`codex-${field.id}-model`} className='shrink-0 text-sm font-medium'>{t(`channels.dialogs.fields.${field.modelLabel}.label`)}</label>
              <Select value={field.model || 'request'} onValueChange={(value) => field.onModel(value === 'request' ? '' : value)}>
                <SelectTrigger id={`codex-${field.id}-model`} className='h-8 w-auto min-w-0 max-w-full gap-2' data-testid={`codex-${field.id}-model-select`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='request'>{t('channels.dialogs.fields.codexSearchModel.followRequest')}</SelectItem>
                  {field.options.map((model) => <SelectItem key={model} value={model}>{model}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
