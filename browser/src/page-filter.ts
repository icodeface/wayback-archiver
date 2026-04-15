// Page filtering logic - determines which pages should be skipped

function normalizeHostname(hostname: string): string {
  return hostname.replace(/^\[/, '').replace(/\]$/, '').toLowerCase();
}

function isPrivateIPv4(hostname: string): boolean {
  const parts = hostname.split('.');
  if (parts.length !== 4) {
    return false;
  }

  const octets = parts.map((part) => Number(part));
  if (octets.some((octet, index) => !/^\d+$/.test(parts[index]) || octet < 0 || octet > 255)) {
    return false;
  }

  const [a, b] = octets;
  return a === 0 ||
    a === 10 ||
    a === 127 ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168);
}

function isPrivateIPv6(hostname: string): boolean {
  const normalized = normalizeHostname(hostname);

  if (normalized === '::1') {
    return true;
  }

  if (normalized.startsWith('::ffff:')) {
    const mapped = normalized.slice('::ffff:'.length);
    if (mapped.includes('.')) {
      return isPrivateIPv4(mapped);
    }

    const hextets = mapped.split(':');
    if (hextets.length === 2) {
      const high = Number.parseInt(hextets[0], 16);
      const low = Number.parseInt(hextets[1], 16);
      if (!Number.isNaN(high) && !Number.isNaN(low)) {
        const mappedIPv4 = [
          (high >> 8) & 0xff,
          high & 0xff,
          (low >> 8) & 0xff,
          low & 0xff,
        ].join('.');
        return isPrivateIPv4(mappedIPv4);
      }
    }

    return false;
  }

  // Only treat actual IPv6 literals as IPv6 addresses. Public hostnames like
  // fd.example.com must not be mistaken for ULA ranges by prefix matching.
  if (!normalized.includes(':')) {
    return false;
  }

  const firstHextet = normalized.split(':').find((part) => part.length > 0);
  if (!firstHextet) {
    return false;
  }

  const value = Number.parseInt(firstHextet, 16);
  if (Number.isNaN(value)) {
    return false;
  }

  return (value >= 0xfc00 && value <= 0xfdff) ||
    (value >= 0xfe80 && value <= 0xfebf);
}

export function shouldSkipURL(rawURL: string): boolean {
  try {
    const parsed = new URL(rawURL);
    const hostname = normalizeHostname(parsed.hostname);

    if (
      parsed.protocol === 'file:' ||
      parsed.protocol === 'about:' ||
      parsed.protocol === 'chrome:' ||
      parsed.protocol === 'chrome-extension:'
    ) {
      return true;
    }

    if (
      hostname === 'localhost' ||
      hostname.endsWith('.local') ||
      isPrivateIPv4(hostname) ||
      isPrivateIPv6(hostname)
    ) {
      return true;
    }
  } catch {
    return false;
  }

  return false;
}

/**
 * Returns true if the current page should not be archived.
 * Skips local pages and browser internal pages.
 */
export function shouldSkipPage(): boolean {
  return shouldSkipURL(window.location.href);
}
