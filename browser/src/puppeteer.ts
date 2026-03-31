// Puppeteer-compatible version of Wayback Archiver
// Uses standard fetch API instead of Tampermonkey GM_xmlhttpRequest

import { CONFIG } from './config';
import { CaptureData } from './types';
import { shouldSkipPage } from './page-filter';
import { waitForDOMStable, serializeCSSOMToDOM } from './page-freezer';
import { inlineLayoutStyles } from './style-inliner';
import { DOMCollector } from './dom-collector';

// pako is loaded via script tag in Puppeteer
declare const pako: any;

interface ArchiveResponse {
  status: string;
  page_id: number;
  action: string;
}

function compressData(data: string): { compressed: Uint8Array; originalSize: number; compressedSize: number } {
  const originalSize = data.length;
  const compressed = pako.gzip(data);
  const compressedSize = compressed.length;
  return { compressed, originalSize, compressedSize };
}

async function sendToServer(captureData: CaptureData): Promise<ArchiveResponse> {
  const jsonData = JSON.stringify(captureData);
  let body: BodyInit;
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };

  if (CONFIG.ENABLE_COMPRESSION) {
    const { compressed, originalSize, compressedSize } = compressData(jsonData);
    const ratio = ((1 - compressedSize / originalSize) * 100).toFixed(1);
    console.log(`[Wayback] >>> Sending (${originalSize} → ${compressedSize} bytes, ${ratio}% reduction)`);
    body = compressed.buffer.slice(compressed.byteOffset, compressed.byteOffset + compressed.byteLength) as ArrayBuffer;
    headers['Content-Encoding'] = 'gzip';
  } else {
    console.log(`[Wayback] >>> Sending (${jsonData.length} bytes)`);
    body = jsonData;
  }

  if (CONFIG.AUTH_PASSWORD) {
    headers['Authorization'] = `Basic ${btoa(`wayback:${CONFIG.AUTH_PASSWORD}`)}`;
  }

  const response = await fetch(CONFIG.SERVER_URL, {
    method: 'POST',
    headers,
    body,
  });

  if (!response.ok) throw new Error(`Archive failed: ${response.status}`);
  const result: ArchiveResponse = await response.json();
  console.log('[Wayback] ✓ Archived:', result.action);
  return result;
}

async function updateOnServer(pageId: number, captureData: CaptureData): Promise<ArchiveResponse> {
  const jsonData = JSON.stringify(captureData);
  const startTime = Date.now();
  let body: BodyInit;
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };

  if (CONFIG.ENABLE_COMPRESSION) {
    const { compressed, originalSize, compressedSize } = compressData(jsonData);
    const ratio = ((1 - compressedSize / originalSize) * 100).toFixed(1);
    console.log(`[Wayback] >>> Updating ${pageId} (${originalSize} → ${compressedSize} bytes, ${ratio}% reduction)`);
    body = compressed.buffer.slice(compressed.byteOffset, compressed.byteOffset + compressed.byteLength) as ArrayBuffer;
    headers['Content-Encoding'] = 'gzip';
  } else {
    console.log(`[Wayback] >>> Updating ${pageId} (${jsonData.length} bytes)`);
    body = jsonData;
  }

  if (CONFIG.AUTH_PASSWORD) {
    headers['Authorization'] = `Basic ${btoa(`wayback:${CONFIG.AUTH_PASSWORD}`)}`;
  }

  const response = await fetch(`${CONFIG.SERVER_URL}/${pageId}`, {
    method: 'PUT',
    headers,
    body,
  });

  const elapsed = Date.now() - startTime;
  if (!response.ok) throw new Error(`Update failed: ${response.status}`);
  const result: ArchiveResponse = await response.json();
  console.log(`[Wayback] ✓ Updated: ${result.action} (${elapsed}ms)`);
  return result;
}

export async function archivePage(): Promise<void> {
  if (shouldSkipPage()) {
    console.log('[Wayback] Skipping page:', window.location.href);
    return;
  }

  console.log('[Wayback] Starting capture:', window.location.href);

  const domCollector = new DOMCollector();
  const collectorObserver = new MutationObserver((mutations) => {
    domCollector.handleMutations(mutations);
  });
  collectorObserver.observe(document.body, { childList: true, subtree: true });

  await waitForDOMStable(CONFIG.MUTATION_OBSERVER_TIMEOUT, CONFIG.DOM_STABLE_TIME);

  serializeCSSOMToDOM();
  let html = inlineLayoutStyles();

  if (domCollector.collectedCount > 0) {
    console.log(`[Wayback] Merging ${domCollector.collectedCount} collected nodes`);
    html = domCollector.mergeInto(html);
  }

  collectorObserver.disconnect();

  const captureData: CaptureData = {
    url: window.location.href,
    title: document.title,
    html,
  };

  await sendToServer(captureData);
  console.log('[Wayback] ✓ Complete');
}

// Export to window for Puppeteer
(window as any).archivePage = archivePage;
