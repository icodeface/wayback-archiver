import { CONFIG } from './config';
import { waitForDOMStable, serializeCSSOMToDOM } from './page-freezer';
import { inlineLayoutStyles } from './style-inliner';
import { normalizeCapturedHTMLURLs } from './html-url-normalizer';
import { createBridgeRequestToken, verifyBridgeRequestToken } from './bridge-auth';
import { FrameCapture } from './types';

const FRAME_ATTR = 'data-wayback-frame-id';
const SOURCE = 'wayback-frame-capture';

type FrameCaptureStatus = 'ok' | 'empty' | 'placeholder' | 'login_required';

type FrameCaptureRequest = {
  source: typeof SOURCE;
  type: 'capture-frame';
  frameId: string;
  parentOrigin: string;
  targetOrigin: string;
  requestId: string;
  timestamp: number;
  token: string;
};

type FrameCaptureResult = {
  source: typeof SOURCE;
  type: 'frame-result';
  requestId: string;
  status: FrameCaptureStatus;
  html: string;
  url: string;
  title: string;
  frames: FrameCapture[];
};

export type DocumentCaptureResult = {
  html: string;
  frames: FrameCapture[];
};

function hasMeaningfulBodyContent(doc: Document): boolean {
  const body = doc.body;
  if (!body) {
    return false;
  }

  if ((body.innerText || '').trim().length > 0) {
    return true;
  }

  return Array.from(body.children).some((el) => {
    const tag = el.tagName.toLowerCase();
    return tag !== 'script' && tag !== 'style' && tag !== 'link' && tag !== 'meta';
  });
}

function normalizeText(text: string): string {
  return text.replace(/\s+/g, ' ').trim().toLowerCase();
}

function isElementHidden(element: Element | null): boolean {
  if (!element) {
    return false;
  }

  const classAttr = element.getAttribute('class') || '';
  if (classAttr.split(/\s+/).some((name) => name.toLowerCase() === 'none')) {
    return true;
  }

  const styleAttr = normalizeText(element.getAttribute('style') || '');
  return /(?:^|[;\s])display\s*:\s*none(?:;|$)/.test(styleAttr) ||
    /(?:^|[;\s])visibility\s*:\s*hidden(?:;|$)/.test(styleAttr);
}

function assessFrameDocument(doc: Document, url: string): FrameCaptureStatus {
  if (!hasMeaningfulBodyContent(doc)) {
    return 'empty';
  }

  if (isElementHidden(doc.body)) {
    return 'placeholder';
  }

  void url;
  return 'ok';
}

async function waitForFrameReady(url: string): Promise<boolean> {
  const deadline = Date.now() + CONFIG.FRAME_CONTENT_WAIT_TIMEOUT;

  while (Date.now() < deadline) {
    if (assessFrameDocument(document, url) === 'ok') {
      return true;
    }
    await new Promise((resolve) => window.setTimeout(resolve, CONFIG.FRAME_CONTENT_CHECK_INTERVAL));
  }

  return false;
}

async function waitForMeaningfulBodyContent(): Promise<boolean> {
  const deadline = Date.now() + CONFIG.FRAME_CONTENT_WAIT_TIMEOUT;

  while (Date.now() < deadline) {
    if (hasMeaningfulBodyContent(document)) {
      return true;
    }
    await new Promise((resolve) => window.setTimeout(resolve, CONFIG.FRAME_CONTENT_CHECK_INTERVAL));
  }

  return false;
}

function isMeaningfulCapturedHTML(html: string): boolean {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  return hasMeaningfulBodyContent(doc);
}

function isCaptureRequest(data: unknown): data is FrameCaptureRequest {
  return !!data && typeof data === 'object' &&
    (data as { source?: string }).source === SOURCE &&
    (data as { type?: string }).type === 'capture-frame' &&
    typeof (data as { frameId?: string }).frameId === 'string' &&
    typeof (data as { parentOrigin?: string }).parentOrigin === 'string' &&
    typeof (data as { targetOrigin?: string }).targetOrigin === 'string' &&
    typeof (data as { requestId?: string }).requestId === 'string' &&
    typeof (data as { timestamp?: number }).timestamp === 'number' &&
    typeof (data as { token?: string }).token === 'string';
}

function isCaptureResult(data: unknown): data is FrameCaptureResult {
  return !!data && typeof data === 'object' &&
    (data as { source?: string }).source === SOURCE &&
    (data as { type?: string }).type === 'frame-result' &&
    typeof (data as { requestId?: string }).requestId === 'string' &&
    typeof (data as { status?: string }).status === 'string' &&
    typeof (data as { html?: string }).html === 'string' &&
    typeof (data as { url?: string }).url === 'string';
}

function dedupeFrames(frames: FrameCapture[]): FrameCapture[] {
  const unique = new Map<string, FrameCapture>();
  for (const frame of frames) {
    if (!frame.key || !frame.url) {
      continue;
    }
    unique.set(frame.key, frame);
  }
  return Array.from(unique.values());
}

function markFrames(): HTMLIFrameElement[] {
  const frames = Array.from(document.querySelectorAll('iframe'));
  for (let i = 0; i < frames.length; i++) {
    const frame = frames[i];
    if (!frame.getAttribute(FRAME_ATTR)) {
      frame.setAttribute(FRAME_ATTR, `wbf-${Date.now()}-${i}-${Math.random().toString(36).slice(2, 8)}`);
    }
  }
  return frames;
}

function resolveFrameOrigin(frame: HTMLIFrameElement): string | null {
  if (frame.hasAttribute('srcdoc')) {
    return window.location.origin;
  }

  const rawSrc = frame.getAttribute('src') || frame.src || 'about:blank';
  if (rawSrc === 'about:blank' || rawSrc === 'about:srcdoc' || rawSrc === '') {
    return window.location.origin;
  }

  try {
    return new URL(rawSrc, window.location.href).origin;
  } catch {
    return null;
  }
}

function embedCapturedFrames(parentHTML: string, capturedFrames: Map<string, FrameCaptureResult>, skippedFrames: Map<string, FrameCaptureResult>): string {
  if (capturedFrames.size === 0 && skippedFrames.size === 0) {
    return parentHTML;
  }

  const doc = new DOMParser().parseFromString(parentHTML, 'text/html');
  const frames = Array.from(doc.querySelectorAll(`iframe[${FRAME_ATTR}]`));

  for (const frame of frames) {
    const frameId = frame.getAttribute(FRAME_ATTR);
    if (!frameId) continue;

    const capture = capturedFrames.get(frameId);
    if (capture) {
      frame.setAttribute('data-wayback-frame-key', frameId);
      frame.setAttribute('data-wayback-original-src', capture.url);
      frame.setAttribute('src', capture.url);
      frame.removeAttribute('srcdoc');
      continue;
    }

    const skipped = skippedFrames.get(frameId);
    if (!skipped) continue;

    frame.setAttribute('data-wayback-frame-status', skipped.status);
    frame.setAttribute('data-wayback-original-src', skipped.url);
    frame.removeAttribute('src');
    frame.removeAttribute('srcdoc');
  }

  return '<!DOCTYPE html>\n' + doc.documentElement.outerHTML;
}

async function requestFrameCapture(frame: HTMLIFrameElement): Promise<FrameCaptureResult | null> {
  const frameId = frame.getAttribute(FRAME_ATTR);
  const frameWindow = frame.contentWindow;
  const targetOrigin = resolveFrameOrigin(frame);
  if (!frameId || !frameWindow || !targetOrigin) {
    return null;
  }

  const requestId = frameId;
  const parentOrigin = window.location.origin;
  const timestamp = Date.now();
  const token = await createBridgeRequestToken(frameId, requestId, parentOrigin, targetOrigin, timestamp);

  return new Promise((resolve) => {
    const channel = new MessageChannel();
    const responsePort = channel.port1;
    const timeoutId = window.setTimeout(() => {
      responsePort.removeEventListener('message', handleMessage);
      responsePort.close();
      resolve(null);
    }, CONFIG.FRAME_CAPTURE_TIMEOUT);

      function handleMessage(event: MessageEvent): void {
        if (!isCaptureResult(event.data) || event.data.requestId !== requestId) {
          return;
        }

        window.clearTimeout(timeoutId);
        responsePort.removeEventListener('message', handleMessage);
        responsePort.close();
        if (event.data.status === 'ok' && !isMeaningfulCapturedHTML(event.data.html)) {
          resolve(null);
          return;
        }
        resolve(event.data);
      }

    responsePort.addEventListener('message', handleMessage);
    responsePort.start();

    try {
      frameWindow.postMessage({
        source: SOURCE,
        type: 'capture-frame',
        frameId,
        parentOrigin,
        targetOrigin,
        requestId,
        timestamp,
        token,
      } satisfies FrameCaptureRequest, targetOrigin, [channel.port2]);
    } catch {
      window.clearTimeout(timeoutId);
      responsePort.removeEventListener('message', handleMessage);
      responsePort.close();
      channel.port2.close();
      resolve(null);
    }
  });
}

export async function captureDocumentHTMLWithFrames(baseURL = window.location.href): Promise<DocumentCaptureResult> {
  const frames = markFrames();
  const capturedFrames = new Map<string, FrameCaptureResult>();
  const skippedFrames = new Map<string, FrameCaptureResult>();
  const collectedFrames: FrameCapture[] = [];

  if (frames.length > 0) {
    const results = await Promise.all(frames.map(requestFrameCapture));
    for (let i = 0; i < results.length; i++) {
      const result = results[i];
      const frameId = frames[i].getAttribute(FRAME_ATTR);
      if (result && frameId) {
        if (result.status === 'ok') {
          capturedFrames.set(frameId, result);
          collectedFrames.push({
            key: frameId,
            url: result.url,
            title: result.title,
            html: result.html,
          });
          collectedFrames.push(...result.frames);
        } else {
          skippedFrames.set(frameId, result);
          console.warn(`[Wayback] Skipping iframe capture (${result.status}):`, result.url);
        }
      }
    }
  }

  serializeCSSOMToDOM();
  const html = normalizeCapturedHTMLURLs(inlineLayoutStyles(), baseURL);
  return {
    html: embedCapturedFrames(html, capturedFrames, skippedFrames),
    frames: dedupeFrames(collectedFrames),
  };
}

export function setupFrameCaptureBridge(): void {
  if (window.self === window.top) {
    return;
  }

  window.addEventListener('message', (event: MessageEvent) => {
    if (!isCaptureRequest(event.data) || event.source !== window.parent || event.ports.length === 0) {
      return;
    }

    void (async () => {
      const replyPort = event.ports[0];
      const { frameId, parentOrigin, requestId, targetOrigin, timestamp, token } = event.data;

      try {
        if (event.origin !== parentOrigin || window.location.origin !== targetOrigin) {
          return;
        }

        if (!await verifyBridgeRequestToken(token, frameId, requestId, parentOrigin, targetOrigin, timestamp)) {
          return;
        }

        try {
          await waitForDOMStable(CONFIG.FRAME_MUTATION_OBSERVER_TIMEOUT, CONFIG.FRAME_DOM_STABLE_TIME);
        } catch {
          // Fall through and capture best-effort current DOM.
        }

        const hasBodyContent = await waitForMeaningfulBodyContent();
        if (hasBodyContent) {
          await waitForFrameReady(window.location.href);
        }

        const captured = await captureDocumentHTMLWithFrames();
        const status = assessFrameDocument(document, window.location.href);
        replyPort.postMessage({
          source: SOURCE,
          type: 'frame-result',
          requestId,
          status,
          html: captured.html,
          url: window.location.href,
          title: document.title,
          frames: captured.frames,
        } satisfies FrameCaptureResult);
      } finally {
        replyPort.close();
      }
    })();
  });
}
