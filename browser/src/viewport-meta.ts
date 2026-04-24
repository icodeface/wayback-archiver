const VIEWPORT_META_NAME = 'wayback-viewport';
const HEAD_TAG_RE = /<head([^>]*)>/i;
const VIEWPORT_META_RE = /<meta\b[^>]*\bname\s*=\s*["']wayback-viewport["'][^>]*>/i;
const VIEWPORT_STYLE_RE = /<style\b[^>]*\bid\s*=\s*["']wayback-captured-viewport-style["'][^>]*>[\s\S]*?<\/style>/i;

export interface CapturedViewportMeta {
  width: number;
  height: number;
  dpr: number;
}

export function captureViewportMeta(): CapturedViewportMeta {
  return {
    width: Math.max(0, Math.round(window.innerWidth || 0)),
    height: Math.max(0, Math.round(window.innerHeight || 0)),
    dpr: Math.max(1, Math.round((window.devicePixelRatio || 1) * 1000) / 1000),
  };
}

export function injectViewportMeta(html: string, viewport: CapturedViewportMeta): string {
  if (!html || viewport.width <= 0 || viewport.height <= 0) {
    return html;
  }

  const metaTag = `<meta name="${VIEWPORT_META_NAME}" content="width=${viewport.width},height=${viewport.height},dpr=${viewport.dpr}">`;
  let result = html;

  // Capture viewport dimensions as metadata only; replay should not rewrite page layout here.
  if (VIEWPORT_STYLE_RE.test(result)) {
    result = result.replace(VIEWPORT_STYLE_RE, '');
  }

  if (VIEWPORT_META_RE.test(result)) {
    result = result.replace(VIEWPORT_META_RE, metaTag);
  } else if (HEAD_TAG_RE.test(result)) {
    result = result.replace(HEAD_TAG_RE, `<head$1>${metaTag}`);
  } else {
    result = metaTag + result;
  }

  return result;
}
