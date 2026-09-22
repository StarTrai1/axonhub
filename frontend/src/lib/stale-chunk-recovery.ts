const CHUNK_RELOAD_KEY = 'axonhub:chunk-reload-attempted';

// A long-lived tab can reference chunks removed by a deployment. The entry
// module URL contains the build hash, so each build gets one recovery attempt
// while a later deployment can still recover in the same tab.
export function installStaleChunkRecovery(buildID: string, browser: Window = window): void {
  let reloading = false;
  const recover = () => {
    if (reloading) return true;
    try {
      if (browser.sessionStorage.getItem(CHUNK_RELOAD_KEY) === buildID) return false;
      browser.sessionStorage.setItem(CHUNK_RELOAD_KEY, buildID);
    } catch {
      // Without a persistent guard a reload could loop. Let the original error
      // surface instead of suppressing it when browser storage is unavailable.
      return false;
    }
    reloading = true;
    browser.location.reload();
    return true;
  };

  browser.addEventListener('vite:preloadError', (event) => {
    if (recover()) event.preventDefault();
  });
  browser.addEventListener('unhandledrejection', (event) => {
    const reason: unknown = event.reason;
    if (
      /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i.test(
        String((reason as { message?: string })?.message ?? reason)
      ) &&
      recover()
    ) {
      event.preventDefault();
    }
  });
}
