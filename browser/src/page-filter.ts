// Page filtering logic - determines which pages should be skipped

/**
 * Returns true if the current page should not be archived.
 * Skips local pages and browser internal pages.
 */
export function shouldSkipPage(): boolean {
  const url = window.location.href;
  const hostname = window.location.hostname;

  // Skip local pages
  if (
    hostname === 'localhost' ||
    hostname === '127.0.0.1' ||
    hostname.startsWith('192.168.') ||
    hostname.startsWith('10.') ||
    hostname.endsWith('.local') ||
    url.startsWith('file://')
  ) {
    return true;
  }

  // Skip browser internal pages
  if (
    url.startsWith('chrome://') ||
    url.startsWith('about:') ||
    url.startsWith('chrome-extension://')
  ) {
    return true;
  }

  return false;
}
