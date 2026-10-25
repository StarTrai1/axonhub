import { useEffect, useMemo, useRef, useState, type InputHTMLAttributes } from 'react';
import { Command as CommandPrimitive } from 'cmdk';
import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';
import { TruncatedText } from './truncated-text';
import { Command, CommandEmpty, CommandGroup, CommandItem, CommandList } from './ui/command';
import { Input } from './ui/input';
import { Popover, PopoverAnchor, PopoverContent } from './ui/popover';
import { Skeleton } from './ui/skeleton';

type Props<T extends string> = {
  selectedValue: T;
  onSelectedValueChange: (value: T) => void;
  items: { value: T; label: string }[];
  isLoading?: boolean;
  emptyMessage?: string;
  placeholder?: string;
  /** 指定 Popover Portal 的容器元素，用于解决在 Dialog 内无法滚动的问题 */
  portalContainer?: HTMLElement | null;
  /** 自定义输入框的 className */
  inputClassName?: string;
  /** 透传给输入框的属性（如表单 a11y：id / aria-invalid / aria-describedby） */
  inputProps?: InputHTMLAttributes<HTMLInputElement>;
  searchValue?: string;
  onSearchValueChange?: (value: string) => void;
  hasMore?: boolean;
  isLoadingMore?: boolean;
  onLoadMore?: () => void | Promise<unknown>;
  onOpenChange?: (open: boolean) => void;
};

// AutoCompleteSelect: strictly selects from provided items. No free-form values are allowed.
export function AutoCompleteSelect<T extends string>({
  selectedValue,
  onSelectedValueChange,
  items,
  isLoading,
  emptyMessage = 'No items.',
  placeholder = 'Search...',
  portalContainer,
  inputClassName,
  inputProps,
  searchValue: controlledSearchValue,
  onSearchValueChange,
  hasMore = false,
  isLoadingMore = false,
  onLoadMore,
  onOpenChange,
}: Props<T>) {
  const [open, setOpen] = useState(false);
  const [internalSearchValue, setInternalSearchValue] = useState('');
  const resolvedSelection = useRef({ value: '', label: '' });
  const searchValue = controlledSearchValue ?? internalSearchValue;
  const setSearchValue = onSearchValueChange ?? setInternalSearchValue;
  const isServerSearch = !!onSearchValueChange;

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    onOpenChange?.(nextOpen);
  };

  // map value -> label for quick lookup
  const labels = useMemo(
    () =>
      items.reduce(
        (acc, item) => {
          acc[item.value] = item.label;
          return acc;
        },
        {} as Record<string, string>
      ),
    [items]
  );

  // Filter items locally based on search string
  const filtered = useMemo(() => {
    if (isServerSearch || !searchValue) return items;
    const q = searchValue.toLowerCase();
    return items.filter((it) => it.label.toLowerCase().includes(q));
  }, [isServerSearch, items, searchValue]);

  const onInputBlur = (e: React.FocusEvent<HTMLInputElement>) => {
    // Strict mode: on blur, revert input text to the selected label
    // unless focus moves within the list. Server-searched options can omit the
    // selected value, so retain its last resolved label across those pages.
    if (!e.relatedTarget?.hasAttribute('cmdk-list')) {
      const label = labels[selectedValue] ?? (resolvedSelection.current.value === selectedValue ? resolvedSelection.current.label : '');
      setSearchValue(selectedValue ? label : '');
    }
  };

  const onSelectItem = (inputValue: string) => {
    // Only accept values from list
    const exists = items.some((it) => it.value === inputValue);
    if (!exists) return;
    resolvedSelection.current = { value: inputValue, label: labels[inputValue] ?? '' };
    onSelectedValueChange(inputValue as T);
    setSearchValue(labels[inputValue] ?? '');
    handleOpenChange(false);
  };

  // Remember the resolved selection across server search pages. Closing an empty
  // search must restore that label, never leave unselected search text in the field.
  useEffect(() => {
    if (labels[selectedValue] !== undefined) {
      resolvedSelection.current = { value: selectedValue, label: labels[selectedValue] };
    }
    if (open) return;
    const label = labels[selectedValue] ?? (resolvedSelection.current.value === selectedValue ? resolvedSelection.current.label : '');
    setSearchValue(selectedValue ? label : '');
  }, [labels, open, selectedValue]);

  return (
    <div className='flex w-full min-w-0 items-center'>
      <Popover open={open} onOpenChange={handleOpenChange}>
        <Command shouldFilter={false} className='w-full bg-transparent'>
          <PopoverAnchor asChild>
            <CommandPrimitive.Input
              asChild
              value={searchValue}
              onValueChange={setSearchValue}
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) {
                  e.stopPropagation();
                  return;
                }
                handleOpenChange(e.key !== 'Escape');
              }}
              onMouseDown={() => {
                if (!open) {
                  setSearchValue('');
                  handleOpenChange(true);
                }
              }}
              onBlur={onInputBlur}
            >
              <Input
                placeholder={placeholder}
                className={cn('w-full', inputClassName)}
                aria-expanded={open}
                // cmdk's empty internal label otherwise overrides the external
                // form label. Explicit caller aria-labelledby still takes priority.
                {...(inputProps?.id || inputProps?.['aria-label'] ? { 'aria-labelledby': undefined } : {})}
                {...inputProps}
              />
            </CommandPrimitive.Input>
          </PopoverAnchor>
          {!open && <CommandList aria-hidden='true' className='hidden' />}
          <PopoverContent
            onEscapeKeyDown={(e) => {
              if (e.isComposing || e.keyCode === 229) e.preventDefault();
            }}
            onOpenAutoFocus={(e) => e.preventDefault()}
            onInteractOutside={(e) => {
              if (e.target instanceof Element && e.target.hasAttribute('cmdk-input')) {
                e.preventDefault();
              }
            }}
            className='w-max max-w-[min(400px,90vw)] min-w-[var(--radix-popover-trigger-width)] p-0'
            container={portalContainer}
          >
            <CommandList hasMore={hasMore} isLoadingMore={isLoadingMore} onLoadMore={onLoadMore}>
              {isLoading && (
                <CommandPrimitive.Loading>
                  <div className='p-1'>
                    <Skeleton className='h-6 w-full' />
                  </div>
                </CommandPrimitive.Loading>
              )}
              {filtered.length > 0 && !isLoading ? (
                <CommandGroup>
                  {filtered.map((option) => (
                    <CommandItem
                      key={option.value}
                      value={option.value}
                      onMouseDown={(e) => e.preventDefault()}
                      onSelect={onSelectItem}
                      className='w-full max-w-full min-w-0'
                    >
                      <Check className={cn('mr-2 h-4 w-4', selectedValue === option.value ? 'opacity-100' : 'opacity-0')} />
                      <TruncatedText className='min-w-0 flex-1'>{option.label}</TruncatedText>
                    </CommandItem>
                  ))}
                </CommandGroup>
              ) : null}
              {!isLoading ? <CommandEmpty>{emptyMessage ?? 'No items.'}</CommandEmpty> : null}
              {isLoadingMore && (
                <CommandPrimitive.Loading>
                  <div className='p-1'>
                    <Skeleton className='h-6 w-full' />
                  </div>
                </CommandPrimitive.Loading>
              )}
            </CommandList>
          </PopoverContent>
        </Command>
      </Popover>
    </div>
  );
}
