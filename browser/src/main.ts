// ==UserScript==
// @name         Wayback Web Archiver
// @namespace    http://tampermonkey.net/
// @version      1.1.0
// @description  Archive web pages to local server
// @author       You
// @match        *://*/*
// @grant        GM_xmlhttpRequest
// @grant        GM_cookie
// @connect      localhost
// @run-at       document-idle
// ==/UserScript==

import { CONFIG } from './config';
import { CaptureData } from './types';
import { shouldSkipPage } from './page-filter';
import { freezePageState, waitForDOMStable, serializeCSSOMToDOM } from './page-freezer';
import { inlineLayoutStyles } from './style-inliner';
import { sendToServer, updateOnServer } from './archiver';
import { DOMCollector } from './dom-collector';

// Early exit check before any initialization
if (shouldSkipPage()) {
  console.log('[Wayback] Skipping page:', window.location.href);
} else {
  console.log('[Wayback] Script loaded for:', window.location.href);
  initializeArchiver();
}

function initializeArchiver(): void {
  // Save native timers before freezePageState() replaces them with noops
  const nativeSetTimeout = window.setTimeout.bind(window);
  const nativeClearTimeout = window.clearTimeout.bind(window);

  let captureData: CaptureData | null = null;
  let isCapturing = false;
  let hasArchived = false;
  let currentPageId: number | null = null;
  let updateCount = 0;
  const MAX_UPDATES = 1;

  // Collects nodes removed by virtual scrolling so we can merge them into the update snapshot
  let domCollector: DOMCollector | null = null;

  // Track active DOM monitor so we can tear it down on SPA navigation
  let activeObserver: MutationObserver | null = null;
  let monitorTimeoutId: number | null = null;

  async function prepareCapture(): Promise<void> {
    if (isCapturing || hasArchived) {
      return;
    }

    isCapturing = true;
    console.log('[Wayback] Preparing capture for:', window.location.href);

    try {
      // Wait for DOM to stabilize
      console.log('[Wayback] Waiting for DOM to stabilize...');
      await waitForDOMStable(CONFIG.MUTATION_OBSERVER_TIMEOUT, CONFIG.DOM_STABLE_TIME);

      // Collect cookies (including HttpOnly) via GM_cookie
      let headers: Record<string, string> | undefined;
      if (typeof GM_cookie !== 'undefined' && GM_cookie.list) {
        try {
          const cookieStr = await new Promise<string>((resolve) => {
            GM_cookie.list({ domain: location.hostname }, (cookies: Array<{ name: string; value: string }>) => {
              resolve(cookies.map(c => `${c.name}=${c.value}`).join('; '));
            });
          });
          if (cookieStr) {
            headers = {
              cookie: cookieStr,
              'user-agent': navigator.userAgent,
            };
          }
        } catch (e) {
          console.warn('[Wayback] GM_cookie not available:', e);
        }
      }

      // Serialize CSSOM without freezing — we need timers alive for the DOM monitor
      serializeCSSOMToDOM();

      // 在克隆 DOM 上内联布局样式，不影响原始页面显示
      const html = inlineLayoutStyles();
      captureData = {
        url: window.location.href,
        title: document.title,
        html,
        headers,
      };

      console.log('[Wayback] ✓ Data prepared, size:', JSON.stringify(captureData).length, 'bytes');
    } catch (error) {
      console.error('[Wayback] Failed to prepare:', error);
    } finally {
      isCapturing = false;
    }
  }

  async function sendCapture(): Promise<void> {
    if (!captureData || hasArchived) {
      return;
    }

    hasArchived = true;
    try {
      const response = await sendToServer(captureData);
      currentPageId = response.page_id;
      console.log('[Wayback] Page ID:', currentPageId, 'Action:', response.action);

      // Start DOM change monitor for newly created pages
      if (response.action === 'created' && updateCount < MAX_UPDATES) {
        startDOMChangeMonitor();
      }
    } catch (error) {
      console.error('[Wayback] Send failed:', error);
    }
  }

  function stopDOMChangeMonitor(): void {
    if (activeObserver) {
      activeObserver.disconnect();
      activeObserver = null;
    }
    if (monitorTimeoutId) {
      nativeClearTimeout(monitorTimeoutId);
      monitorTimeoutId = null;
    }
  }

  function startDOMChangeMonitor(): void {
    // Clean up any previous monitor first
    stopDOMChangeMonitor();

    console.log('[Wayback] Starting DOM change monitor...');

    // Create a collector to accumulate nodes removed by virtual scrolling
    domCollector = new DOMCollector();

    let mutationCount = 0;
    let debounceTimer: number | null = null;
    // Snapshot the page ID at monitor start so the callback can't act on a different page
    const monitorPageId = currentPageId;

    const observer = new MutationObserver((mutations) => {
      mutationCount += mutations.length;

      // Feed every mutation to the collector so it tracks removed/re-added nodes
      domCollector?.handleMutations(mutations);

      if (debounceTimer) {
        nativeClearTimeout(debounceTimer);
      }

      debounceTimer = nativeSetTimeout(async () => {
        // Guard: only proceed if the page ID hasn't changed (SPA navigation resets it)
        if (mutationCount >= CONFIG.UPDATE_MIN_MUTATIONS && monitorPageId && monitorPageId === currentPageId && updateCount < MAX_UPDATES) {
          console.log(`[Wayback] DOM changed (${mutationCount} mutations), triggering update...`);
          updateCount++;
          stopDOMChangeMonitor();

          try {
            await waitForDOMStable(CONFIG.MUTATION_OBSERVER_TIMEOUT, CONFIG.DOM_STABLE_TIME);

            // Only serialize CSSOM — don't freeze, to keep the SPA functional
            serializeCSSOMToDOM();

            // 在克隆 DOM 上内联布局样式
            let newHTML = inlineLayoutStyles();

            // Merge any nodes that were removed by virtual scrolling back into the snapshot
            if (domCollector && domCollector.collectedCount > 0) {
              console.log(`[Wayback] Merging ${domCollector.collectedCount} collected nodes...`);
              newHTML = domCollector.mergeInto(newHTML);
            }

            const newCaptureData: CaptureData = {
              url: window.location.href,
              title: document.title,
              html: newHTML,
              headers: captureData?.headers,
            };

            await updateOnServer(monitorPageId, newCaptureData);
          } catch (error) {
            console.error('[Wayback] Update failed:', error);
          }
        }
        mutationCount = 0;
      }, CONFIG.UPDATE_DEBOUNCE_DELAY) as unknown as number;
    });

    activeObserver = observer;

    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: false,
      characterData: false,
    });

    // Auto-stop after timeout
    monitorTimeoutId = nativeSetTimeout(() => {
      stopDOMChangeMonitor();
      console.log('[Wayback] DOM change monitor stopped');
    }, CONFIG.UPDATE_MONITOR_TIMEOUT) as unknown as number;
  }

  function resetState(): void {
    stopDOMChangeMonitor();
    captureData = null;
    isCapturing = false;
    hasArchived = false;
    currentPageId = null;
    updateCount = 0;
    if (domCollector) {
      domCollector.clear();
      domCollector = null;
    }
  }

  // Start capture after initial delay
  console.log('[Wayback] Initializing...');
  nativeSetTimeout(async () => {
    await prepareCapture();
    if (captureData) {
      await sendCapture();
    }
  }, CONFIG.DOM_STABILITY_DELAY);

  // Send on page unload events
  window.addEventListener('beforeunload', () => {
    console.log('[Wayback] beforeunload');
    sendCapture();
  });

  window.addEventListener('pagehide', () => {
    console.log('[Wayback] pagehide');
    sendCapture();
  });

  // Handle SPA navigation — send current capture, then re-capture the new page
  if ((window as unknown as Record<string, unknown>).navigation) {
    (window as unknown as { navigation: { addEventListener: (event: string, handler: (e: { navigationType: string }) => void) => void } })
      .navigation.addEventListener('navigate', (e) => {
        // Only handle push/replace (SPA navigations), not traverse (back/forward) or reload
        if (e.navigationType === 'traverse' || e.navigationType === 'reload') {
          return;
        }
        console.log('[Wayback] SPA navigate detected');
        sendCapture();
        resetState();
        // Wait for new page to render, then capture
        nativeSetTimeout(async () => {
          await prepareCapture();
          if (captureData) {
            await sendCapture();
          }
        }, CONFIG.DOM_STABILITY_DELAY);
      });
  }

  // Fallback: detect URL changes via pushState/replaceState for browsers without Navigation API
  const originalPushState = history.pushState.bind(history);
  const originalReplaceState = history.replaceState.bind(history);
  let lastURL = window.location.href;

  function onURLChange(): void {
    const newURL = window.location.href;
    if (newURL === lastURL) return;
    console.log('[Wayback] URL changed:', lastURL, '->', newURL);
    lastURL = newURL;
    sendCapture();
    resetState();
    nativeSetTimeout(async () => {
      await prepareCapture();
      if (captureData) {
        await sendCapture();
      }
    }, CONFIG.DOM_STABILITY_DELAY);
  }

  if (!(window as unknown as Record<string, unknown>).navigation) {
    history.pushState = function(...args: Parameters<typeof history.pushState>) {
      originalPushState(...args);
      onURLChange();
    };
    history.replaceState = function(...args: Parameters<typeof history.replaceState>) {
      originalReplaceState(...args);
      onURLChange();
    };
    window.addEventListener('popstate', () => onURLChange());
  }

  // Send when page becomes hidden
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      console.log('[Wayback] hidden');
      sendCapture();
    }
  });
}
