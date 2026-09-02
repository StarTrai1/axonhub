import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { useScheduledChannelHealthCheckResults } from '../data/channels';

const resultCursorStorageKey = 'scheduled-channel-health-check-result-cursor';

export function ScheduledHealthCheckNotifier() {
  const { t } = useTranslation();
  const [isVisible, setIsVisible] = useState(() => !document.hidden);
  const hasStoredCursor = useRef(localStorage.getItem(resultCursorStorageKey) !== null);
  const [cursor, setCursor] = useState(() => {
    const stored = Number(localStorage.getItem(resultCursorStorageKey));
    return Number.isSafeInteger(stored) && stored > 0 ? stored : 0;
  });
  const initialized = useRef(false);
  const seen = useRef(new Set<number>());
  const { data } = useScheduledChannelHealthCheckResults(cursor);

  useEffect(() => {
    const handleVisibilityChange = () => setIsVisible(!document.hidden);
    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange);
  }, []);

  useEffect(() => {
    // Browsers throttle hidden tabs and a toast emitted while Windows is locked
    // expires before the user can see it. Keep the server cursor unchanged while
    // hidden, then surface all retained results as soon as the page is visible.
    if (!data || !isVisible) {
      return;
    }

    if (!initialized.current) {
      initialized.current = true;
      if (!hasStoredCursor.current) {
        data.results.forEach((result) => seen.current.add(result.id));
        localStorage.setItem(resultCursorStorageKey, String(data.latestID));
        setCursor(data.latestID);
        return;
      }
    }

    for (const result of data.results) {
      if (seen.current.has(result.id)) {
        continue;
      }
      seen.current.add(result.id);
      if (result.success) {
        toast.success(
          t('channels.dialogs.scheduledHealthCheck.notificationSuccess', {
            name: result.channelName,
            latency: result.latency.toFixed(2),
          })
        );
      } else {
        toast.error(t('channels.dialogs.scheduledHealthCheck.notificationFailure', { name: result.channelName }), {
          description: result.error || t('common.errors.internalServerError'),
          duration: 5000,
        });
      }
    }

    if (data.latestID > cursor) {
      localStorage.setItem(resultCursorStorageKey, String(data.latestID));
      setCursor(data.latestID);
    }
  }, [cursor, data, isVisible, t]);

  return null;
}
