const URL_ATTRS = ['src', 'href', 'poster'];
const SRCSET_ATTRS = ['srcset', 'imagesrcset'];

function shouldSkipURL(value: string): boolean {
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
  if (shouldSkipURL(trimmed)) {
    return value;
  }

  try {
    return new URL(trimmed, baseURL).toString();
  } catch {
    return value;
  }
}

function normalizeSrcset(value: string, baseURL: string): string {
  return value
    .split(',')
    .map((part) => {
      const trimmed = part.trim();
      if (!trimmed) {
        return part;
      }

      const fields = trimmed.split(/\s+/);
      fields[0] = toAbsoluteURL(fields[0], baseURL);
      return fields.join(' ');
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
