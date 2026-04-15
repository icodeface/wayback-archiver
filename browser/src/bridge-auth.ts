const BRIDGE_SECRET_STORAGE_KEY = 'wayback-bridge-secret-v1';
const BRIDGE_REQUEST_MAX_AGE_MS = 15000;
const BRIDGE_SECRET_SETTLE_MS = 75;
const FALLBACK_BRIDGE_SECRET = 'wayback-puppeteer-bridge-secret-v1';

let cachedBridgeSecret: Promise<string> | null = null;
let cachedBridgeKey: Promise<CryptoKey> | null = null;
let cachedBridgeKeySecret = '';
let bridgeSecretListenerRegistered = false;

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

function generateBridgeSecret(): string {
  const bytes = new Uint8Array(32);
  if (crypto && typeof crypto.getRandomValues === 'function') {
    crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i++) {
      bytes[i] = Math.floor(Math.random() * 256);
    }
  }
  return bytesToHex(bytes);
}

function fallbackHash(input: string): string {
  let h1 = 0xdeadbeef;
  let h2 = 0x41c6ce57;

  for (let i = 0; i < input.length; i++) {
    const ch = input.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }

  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909);

  return [(h2 >>> 0).toString(16).padStart(8, '0'), (h1 >>> 0).toString(16).padStart(8, '0')].join('');
}

function buildBridgePayload(
  frameId: string,
  requestId: string,
  parentOrigin: string,
  targetOrigin: string,
  timestamp: number,
): string {
  return [
    'wayback-frame-capture-v1',
    frameId,
    requestId,
    parentOrigin,
    targetOrigin,
    String(timestamp),
  ].join('\n');
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function syncCachedBridgeSecret(secret: string): void {
  cachedBridgeSecret = Promise.resolve(secret);
  if (cachedBridgeKeySecret !== secret) {
    cachedBridgeKey = null;
    cachedBridgeKeySecret = '';
  }
}

function registerBridgeSecretListener(): void {
  if (bridgeSecretListenerRegistered || typeof GM_addValueChangeListener !== 'function') {
    return;
  }

  GM_addValueChangeListener(BRIDGE_SECRET_STORAGE_KEY, (_name, _oldValue, newValue) => {
    if (typeof newValue === 'string' && newValue) {
      syncCachedBridgeSecret(newValue);
    }
  });
  bridgeSecretListenerRegistered = true;
}

async function getBridgeSecret(): Promise<string> {
  if (cachedBridgeSecret) {
    return cachedBridgeSecret;
  }

  cachedBridgeSecret = (async () => {
    if (typeof GM_getValue === 'function' && typeof GM_setValue === 'function') {
      registerBridgeSecretListener();

      const existing = GM_getValue(BRIDGE_SECRET_STORAGE_KEY, '');
      if (typeof existing === 'string' && existing) {
        return existing;
      }

      const generated = generateBridgeSecret();
      GM_setValue(BRIDGE_SECRET_STORAGE_KEY, generated);

      // Multiple tabs can race on first-time initialization. Re-read after a
      // short settle window so every page converges on the stored value.
      await sleep(BRIDGE_SECRET_SETTLE_MS);
      const settled = GM_getValue(BRIDGE_SECRET_STORAGE_KEY, '');
      if (typeof settled === 'string' && settled) {
        return settled;
      }

      return generated;
    }

    return FALLBACK_BRIDGE_SECRET;
  })()
    .then((secret) => {
      syncCachedBridgeSecret(secret);
      return secret;
    })
    .catch((error) => {
      cachedBridgeSecret = null;
      throw error;
    });

  return cachedBridgeSecret;
}

async function getBridgeKey(): Promise<CryptoKey> {
  if (!crypto || !crypto.subtle) {
    throw new Error('subtle crypto unavailable');
  }

  const secret = await getBridgeSecret();
  if (cachedBridgeKey && cachedBridgeKeySecret === secret) {
    return cachedBridgeKey;
  }

  cachedBridgeKeySecret = secret;
  cachedBridgeKey = crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );

  return cachedBridgeKey;
}

export function isBridgeRequestFresh(timestamp: number): boolean {
  return Number.isFinite(timestamp) && Math.abs(Date.now() - timestamp) <= BRIDGE_REQUEST_MAX_AGE_MS;
}

export async function createBridgeRequestToken(
  frameId: string,
  requestId: string,
  parentOrigin: string,
  targetOrigin: string,
  timestamp: number,
): Promise<string> {
  const payload = buildBridgePayload(frameId, requestId, parentOrigin, targetOrigin, timestamp);
  const secret = await getBridgeSecret();

  if (!crypto || !crypto.subtle) {
    return fallbackHash(`${secret}\n${payload}`);
  }

  const key = await getBridgeKey();
  const signature = await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(payload));
  return bytesToHex(new Uint8Array(signature));
}

export async function verifyBridgeRequestToken(
  token: string,
  frameId: string,
  requestId: string,
  parentOrigin: string,
  targetOrigin: string,
  timestamp: number,
): Promise<boolean> {
  if (!token || !isBridgeRequestFresh(timestamp)) {
    return false;
  }

  const expected = await createBridgeRequestToken(frameId, requestId, parentOrigin, targetOrigin, timestamp);
  return expected === token;
}
