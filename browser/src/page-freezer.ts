// Page freezing logic - prevents dynamic updates during capture

import { CONFIG } from './config';

// Save native timers before freezePageState() overwrites them
const nativeSetTimeout = window.setTimeout.bind(window);
const nativeClearTimeout = window.clearTimeout.bind(window);

/**
 * Freezes the page state to prevent dynamic updates during capture.
 * Clears existing timers, disables new timers, and blocks WebSocket/EventSource.
 */
export function freezePageState(): void {
  console.log('[Wayback] Freezing page state...');

  // Clear all existing timers
  for (let i = 1; i < CONFIG.TIMER_CLEAR_RANGE; i++) {
    try {
      clearInterval(i);
      clearTimeout(i);
    } catch (e) {
      // ignore errors
    }
  }

  // Disable subsequent timers
  const noop = () => -1;
  window.setTimeout = noop as unknown as typeof window.setTimeout;
  window.setInterval = noop as unknown as typeof window.setInterval;
  window.requestAnimationFrame = noop as unknown as typeof window.requestAnimationFrame;

  // Block WebSocket to prevent real-time updates
  if (window.WebSocket) {
    (window as unknown as Record<string, unknown>).WebSocket = function() {
      console.log('[Wayback] WebSocket blocked');
      return {
        close: () => {},
        send: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        readyState: 3, // CLOSED
        CONNECTING: 0,
        OPEN: 1,
        CLOSING: 2,
        CLOSED: 3
      };
    };
  }

  // Block EventSource (SSE)
  if ((window as unknown as Record<string, unknown>).EventSource) {
    (window as unknown as Record<string, unknown>).EventSource = function() {
      console.log('[Wayback] EventSource blocked');
      return {
        close: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        readyState: 2, // CLOSED
        CONNECTING: 0,
        OPEN: 1,
        CLOSED: 2
      };
    };
  }

  // Serialize CSSOM rules into DOM so outerHTML captures them
  serializeCSSOM();

  console.log('[Wayback] Page state frozen');
}

/**
 * Serializes CSSOM-injected CSS rules back into <style> elements.
 * Sites like X/Twitter use CSSStyleSheet.insertRule() to inject styles,
 * which don't appear in <style> innerHTML and are lost by outerHTML.
 */
export function serializeCSSOMToDOM(): void {
  serializeCSSOM();
}

function serializeCSSOM(): void {
  let serialized = 0;
  try {
    for (let i = 0; i < document.styleSheets.length; i++) {
      const sheet = document.styleSheets[i];
      // Skip external stylesheets (cross-origin will throw on cssRules)
      if (sheet.href) continue;

      let rules: CSSRuleList;
      try {
        rules = sheet.cssRules;
      } catch {
        continue; // cross-origin or security error
      }

      const ownerNode = sheet.ownerNode;
      if (!(ownerNode instanceof HTMLStyleElement)) continue;

      // Build the full CSS text from CSSOM rules
      const parts: string[] = [];
      for (let j = 0; j < rules.length; j++) {
        parts.push(rules[j].cssText);
      }
      const cssom = parts.join('\n');

      // If the CSSOM has more content than the DOM textContent, overwrite
      const domText = ownerNode.textContent || '';
      if (cssom.length > domText.length) {
        ownerNode.textContent = cssom;
        serialized++;
      }
    }
  } catch (e) {
    console.warn('[Wayback] CSSOM serialization error:', e);
  }
  if (serialized > 0) {
    console.log(`[Wayback] Serialized ${serialized} CSSOM stylesheets into DOM`);
  }
}

/**
 * Waits for the DOM to stabilize (no mutations for stableTime ms).
 * Resolves after timeout regardless.
 */
export function waitForDOMStable(
  timeout = CONFIG.MUTATION_OBSERVER_TIMEOUT,
  stableTime = CONFIG.DOM_STABLE_TIME
): Promise<void> {
  return new Promise((resolve) => {
    let timer: number | null = null;
    let timeoutId: number | null = null;
    let lastMutation = Date.now();
    let resolved = false;

    const cleanup = () => {
      if (timer) nativeClearTimeout(timer);
      if (timeoutId) nativeClearTimeout(timeoutId);
      observer.disconnect();
    };

    const observer = new MutationObserver(() => {
      lastMutation = Date.now();
      if (timer) nativeClearTimeout(timer);

      timer = nativeSetTimeout(() => {
        const elapsed = Date.now() - lastMutation;
        if (elapsed >= stableTime && !resolved) {
          resolved = true;
          cleanup();
          console.log('[Wayback] DOM stable after', elapsed, 'ms');
          resolve();
        }
      }, stableTime) as unknown as number;
    });

    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      characterData: true
    });

    // Timeout protection
    timeoutId = nativeSetTimeout(() => {
      if (!resolved) {
        resolved = true;
        cleanup();
        console.log('[Wayback] DOM stability timeout reached');
        resolve();
      }
    }, timeout) as unknown as number;
  });
}
