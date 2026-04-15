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
import { waitForDOMStable } from './page-freezer';
import { sendToServer, updateOnServer } from './archiver';
import { DOMCollector } from './dom-collector';
import { captureDocumentHTMLWithFrames, setupFrameCaptureBridge } from './frame-capture';

// Early exit check before any initialization
if (shouldSkipPage()) {
  console.log('[Wayback] Skipping page:', window.location.href);
} else {
  console.log('[Wayback] Script loaded for:', window.location.href);
  if (window.self === window.top) {
    initializeArchiver();
  } else {
    setupFrameCaptureBridge();
  }
}

function initializeArchiver(): void {
  const nativeSetTimeout = window.setTimeout.bind(window);
  const nativeClearTimeout = window.clearTimeout.bind(window);
  const nativeSetInterval = window.setInterval.bind(window);
  const nativeClearInterval = window.clearInterval.bind(window);

  let captureData: CaptureData | null = null;
  let isCapturing = false;
  let hasArchived = false;
  let sendPromise: Promise<void> | null = null;
  let currentPageId: number | null = null;
  let initialHTMLSize = 0; // Track initial capture size for update quality guard

  // Collects nodes removed by virtual scrolling so we can merge them into snapshots
  const domCollector = new DOMCollector();
  let collectorObserver: MutationObserver | null = null;

  // Track active DOM monitor so we can tear it down on SPA navigation
  let activeObserver: MutationObserver | null = null;
  let monitorTimeoutId: number | null = null;
  let monitorIntervalId: number | null = null;
  // Track pending SPA transition timers so we can cancel them on rapid re-navigation
  let spaCollectorTimerId: number | null = null;
  let spaCaptureTimerId: number | null = null;

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

      // 在克隆 DOM 上内联布局样式，并把 iframe 当前内容嵌入快照
      const captured = await captureDocumentHTMLWithFrames();
      let html = captured.html;

      // Merge any nodes removed by virtual scrolling before/during capture
      if (domCollector.collectedCount > 0) {
        console.log(`[Wayback] Merging ${domCollector.collectedCount} collected nodes into initial capture...`);
        html = domCollector.mergeInto(html);
      }

      captureData = {
        url: window.location.href,
        title: document.title,
        html,
        frames: captured.frames,
        headers,
      };

      initialHTMLSize = html.length;
      console.log('[Wayback] ✓ Data prepared, size:', JSON.stringify(captureData).length, 'bytes');
    } catch (error) {
      console.error('[Wayback] Failed to prepare:', error);
    } finally {
      isCapturing = false;
    }
  }

  function sendCapture(): Promise<void> {
    if (!captureData || hasArchived) {
      return sendPromise || Promise.resolve();
    }

    if (sendPromise) {
      return sendPromise;
    }

    const pendingCapture = captureData;

    sendPromise = (async () => {
      try {
        const response = await sendToServer(pendingCapture);
        currentPageId = response.page_id;
        hasArchived = true;
        console.log('[Wayback] Page ID:', currentPageId, 'Action:', response.action);

        // Even when POST returns unchanged, we still need to watch for later
        // DOM mutations so dynamic content can upgrade the existing snapshot.
        if (response.action === 'created' || response.action === 'unchanged') {
          startDOMChangeMonitor();
        }
      } catch (error) {
        console.error('[Wayback] Send failed:', error);
      } finally {
        sendPromise = null;
      }
    })();

    return sendPromise;
  }

  function stopDOMChangeMonitor(): void {
    if (activeObserver) {
      activeObserver.disconnect();
      activeObserver = null;
    }
    if (monitorIntervalId) {
      nativeClearInterval(monitorIntervalId);
      monitorIntervalId = null;
    }
    if (monitorTimeoutId) {
      nativeClearTimeout(monitorTimeoutId);
      monitorTimeoutId = null;
    }
  }

  function startDOMChangeMonitor(): void {
    // Clean up any previous monitor first
    stopDOMChangeMonitor();

    console.log('[Wayback] Starting DOM change monitor (interval mode)...');

    // Disconnect the collector-only observer — the new monitor observer takes over feeding domCollector
    if (collectorObserver) {
      collectorObserver.disconnect();
      collectorObserver = null;
    }
    // Do NOT clear domCollector — it may contain nodes removed between initial capture and now
    // (e.g. the main tweet scrolled out during sendCapture's network request)

    let mutationCount = 0;
    let isUpdating = false;
    // Snapshot the page ID at monitor start so the callback can't act on a different page
    const monitorPageId = currentPageId;

    // Track the furthest scroll position — only upload when user reaches new content
    let maxScrollY = window.scrollY;

    const observer = new MutationObserver((mutations) => {
      mutationCount += mutations.length;
      // Feed every mutation to the collector so it tracks removed/re-added nodes
      domCollector.handleMutations(mutations);
    });

    activeObserver = observer;

    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: false,
      characterData: false,
    });

    // Periodic check: every UPDATE_CHECK_INTERVAL, upload if DOM has changed
    const intervalId = nativeSetInterval(() => {
      if (isUpdating) return;

      // Guard: page ID changed (SPA navigation) — stop
      if (!monitorPageId || monitorPageId !== currentPageId) {
        stopDOMChangeMonitor();
        return;
      }

      // Collector reached size limit — do one final upload and stop
      if (domCollector.reachedLimit) {
        console.log('[Wayback] Collector reached size limit, performing final update...');
        mutationCount = Math.max(mutationCount, CONFIG.UPDATE_MIN_MUTATIONS); // force trigger
      }

      // No meaningful changes — skip this cycle
      if (mutationCount < CONFIG.UPDATE_MIN_MUTATIONS) {
        return;
      }

      // Guard: skip update when tab is hidden — sites like X.com aggressively strip DOM
      if (document.visibilityState === 'hidden') {
        console.log(`[Wayback] Skipping update: tab is hidden (DOM may be stripped)`);
        return;
      }

      // Guard: skip update if user hasn't scrolled past previous max — content already captured
      const currentScrollY = window.scrollY;
      if (currentScrollY < maxScrollY) {
        console.log(`[Wayback] Skipping update: not at new scroll position (${currentScrollY} < ${maxScrollY})`);
        return;
      }
      maxScrollY = currentScrollY;

      const currentMutations = mutationCount;
      mutationCount = 0;
      isUpdating = true;
      const isFinal = domCollector.reachedLimit;

      (async () => {
        try {
          console.log(`[Wayback] DOM changed (${currentMutations} mutations), triggering update...`);

          // 在克隆 DOM 上内联布局样式，并把 iframe 当前内容嵌入快照
          const captured = await captureDocumentHTMLWithFrames();
          let newHTML = captured.html;

          // Merge any nodes that were removed by virtual scrolling back into the snapshot
          if (domCollector.collectedCount > 0) {
            console.log(`[Wayback] Merging ${domCollector.collectedCount} collected nodes...`);
            newHTML = domCollector.mergeInto(newHTML);
          }

          // Guard: reject update if HTML shrunk significantly (< 70% of initial capture)
          if (initialHTMLSize > 0 && newHTML.length < initialHTMLSize * 0.7) {
            console.log(`[Wayback] Skipping update: HTML shrunk too much (${newHTML.length} vs initial ${initialHTMLSize}, ${Math.round(newHTML.length / initialHTMLSize * 100)}%)`);
            return;
          }

          const newCaptureData: CaptureData = {
            url: window.location.href,
            title: document.title,
            html: newHTML,
            frames: captured.frames,
            headers: captureData?.headers,
          };

          await updateOnServer(monitorPageId, newCaptureData);
        } catch (error) {
          console.error('[Wayback] Update failed:', error);
        } finally {
          if (isFinal) {
            console.log('[Wayback] Final update cycle complete, stopping monitor');
            stopDOMChangeMonitor();
          }
          isUpdating = false;
        }
      })();
    }, CONFIG.UPDATE_CHECK_INTERVAL) as unknown as number;

    // Store intervalId so stopDOMChangeMonitor can clear it
    monitorIntervalId = intervalId;

    // Auto-stop after timeout
    monitorTimeoutId = nativeSetTimeout(() => {
      stopDOMChangeMonitor();
      console.log('[Wayback] DOM change monitor stopped (timeout)');
    }, CONFIG.UPDATE_MONITOR_TIMEOUT) as unknown as number;
  }

  function startCollectorObserver(): void {
    if (collectorObserver) {
      collectorObserver.disconnect();
    }
    collectorObserver = new MutationObserver((mutations) => {
      domCollector.handleMutations(mutations);
    });
    collectorObserver.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: false,
      characterData: false,
    });
  }

  function resetState(): void {
    stopDOMChangeMonitor();
    captureData = null;
    isCapturing = false;
    hasArchived = false;
    sendPromise = null;
    currentPageId = null;
    initialHTMLSize = 0;
    if (collectorObserver) {
      collectorObserver.disconnect();
      collectorObserver = null;
    }
    domCollector.clear();
    // Cancel pending SPA transition timers from a previous navigation
    if (spaCollectorTimerId !== null) {
      nativeClearTimeout(spaCollectorTimerId);
      spaCollectorTimerId = null;
    }
    if (spaCaptureTimerId !== null) {
      nativeClearTimeout(spaCaptureTimerId);
      spaCaptureTimerId = null;
    }
    // Do NOT start collector here — old page DOM is still being torn down by the SPA framework.
    // Collector will be restarted after SPA_TRANSITION_DELAY, when only the new page's DOM remains.
  }

  // Start capture after initial delay
  console.log('[Wayback] Initializing...');
  // Start collector immediately for initial page load to catch virtual scroll removals
  startCollectorObserver();
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
        // Skip reload — page will fully reload and script re-initializes
        if (e.navigationType === 'reload') {
          return;
        }
        console.log('[Wayback] SPA navigate detected:', e.navigationType);
        // 等待 sendCapture 完成后再重置状态，防止竞态条件
        sendCapture().then(() => {
          resetState();
          // Start collector early (after SPA transition completes) to catch virtual scroll removals
          spaCollectorTimerId = nativeSetTimeout(() => {
            spaCollectorTimerId = null;
            startCollectorObserver();
          }, CONFIG.SPA_TRANSITION_DELAY) as unknown as number;
          // Wait for new page to fully render, then capture
          spaCaptureTimerId = nativeSetTimeout(async () => {
            spaCaptureTimerId = null;
            await prepareCapture();
            if (captureData) {
              await sendCapture();
            }
          }, CONFIG.DOM_STABILITY_DELAY) as unknown as number;
        });
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
    // 等待 sendCapture 完成后再重置状态，防止竞态条件
    sendCapture().then(() => {
      resetState();
      // Start collector early (after SPA transition completes) to catch virtual scroll removals
      spaCollectorTimerId = nativeSetTimeout(() => {
        spaCollectorTimerId = null;
        startCollectorObserver();
      }, CONFIG.SPA_TRANSITION_DELAY) as unknown as number;
      // Wait for new page to fully render, then capture
      spaCaptureTimerId = nativeSetTimeout(async () => {
        spaCaptureTimerId = null;
        await prepareCapture();
        if (captureData) {
          await sendCapture();
        }
      }, CONFIG.DOM_STABILITY_DELAY) as unknown as number;
    });
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
