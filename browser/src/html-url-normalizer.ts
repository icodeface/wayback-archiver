const URL_ATTRS = ['src', 'href', 'poster'];
const SRCSET_ATTRS = ['srcset', 'imagesrcset'];

function shouldSkipCapturedURL(value: string): boolean {
  return value === '' ||
    value.startsWith('data:') ||
    value.startsWith('javascript:') ||
    value.startsWith('mailto:') ||
    value.startsWith('tel:') ||
    value.startsWith('blob:') ||
    value.startsWith('#');
}

function toAbsoluteURL(value: string, baseURL: string): string {
  const trimmed = value.trim();
  if (shouldSkipCapturedURL(trimmed)) {
    return value;
  }

  try {
    return new URL(trimmed, baseURL).toString();
  } catch {
    return value;
  }
}

// Parse srcset per HTML spec: URLs are non-whitespace sequences, so commas
// without surrounding whitespace are part of the URL (e.g. OSS query params).
function parseSrcsetCandidates(srcset: string): Array<{ url: string; descriptor: string }> {
  const candidates: Array<{ url: string; descriptor: string }> = [];
  let i = 0;
  const n = srcset.length;

  while (i < n) {
    // Skip whitespace and commas (separators)
    while (i < n && (srcset[i] === ' ' || srcset[i] === '\t' || srcset[i] === '\n' ||
      srcset[i] === '\r' || srcset[i] === '\f' || srcset[i] === ',')) {
      i++;
    }
    if (i >= n) break;

    // Collect non-whitespace → URL
    const start = i;
    while (i < n && srcset[i] !== ' ' && srcset[i] !== '\t' && srcset[i] !== '\n' &&
      srcset[i] !== '\r' && srcset[i] !== '\f') {
      i++;
    }
    let url = srcset.slice(start, i).replace(/,+$/, '');
    if (!url) continue;

    // Skip whitespace after URL
    while (i < n && (srcset[i] === ' ' || srcset[i] === '\t' || srcset[i] === '\n' ||
      srcset[i] === '\r' || srcset[i] === '\f')) {
      i++;
    }

    // Collect descriptor until comma or end
    const descStart = i;
    while (i < n && srcset[i] !== ',') {
      i++;
    }
    const descriptor = srcset.slice(descStart, i).trim();

    candidates.push({ url, descriptor });
  }

  return candidates;
}

function normalizeSrcset(value: string, baseURL: string): string {
  const candidates = parseSrcsetCandidates(value);
  return candidates
    .map((c) => {
      const absoluteURL = toAbsoluteURL(c.url, baseURL);
      return c.descriptor ? `${absoluteURL} ${c.descriptor}` : absoluteURL;
    })
    .join(', ');
}

function normalizeStyleURLs(styleValue: string, baseURL: string): string {
  return styleValue.replace(/url\((['"]?)([^)'"\s]+)\1\)/gi, (match, quote: string, rawURL: string) => {
    const absoluteURL = toAbsoluteURL(rawURL, baseURL);
    if (absoluteURL === rawURL) {
      return match;
    }
    const q = quote || '"';
    return `url(${q}${absoluteURL}${q})`;
  });
}

export function normalizeCapturedHTMLURLs(html: string, baseURL: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html');

  for (const element of Array.from(doc.querySelectorAll('*'))) {
    for (const attr of URL_ATTRS) {
      const value = element.getAttribute(attr);
      if (value) {
        element.setAttribute(attr, toAbsoluteURL(value, baseURL));
      }
    }

    for (const attr of SRCSET_ATTRS) {
      const value = element.getAttribute(attr);
      if (value) {
        element.setAttribute(attr, normalizeSrcset(value, baseURL));
      }
    }

    const style = element.getAttribute('style');
    if (style) {
      element.setAttribute('style', normalizeStyleURLs(style, baseURL));
    }
  }

  for (const styleElement of Array.from(doc.querySelectorAll('style'))) {
    if (styleElement.textContent) {
      styleElement.textContent = normalizeStyleURLs(styleElement.textContent, baseURL);
    }
  }

  return '<!DOCTYPE html>\n' + doc.documentElement.outerHTML;
}
