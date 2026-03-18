// Archiver - sends captured data to the server

// pako is loaded via @require in Tampermonkey header
declare const pako: any;

import { CONFIG } from './config';
import { CaptureData, ArchiveResponse } from './types';

/**
 * Compresses data using gzip and returns binary string for transmission.
 * Returns both the compressed data and compression stats.
 */
function compressData(data: string): { compressed: string; originalSize: number; compressedSize: number } {
  const originalSize = data.length;
  const compressed = pako.gzip(data);
  const compressedSize = compressed.length;

  // Convert Uint8Array to binary string for GM_xmlhttpRequest
  // Each character's charCode represents one byte
  let binary = '';
  for (let i = 0; i < compressed.length; i++) {
    binary += String.fromCharCode(compressed[i]);
  }

  return { compressed: binary, originalSize, compressedSize };
}

/**
 * Sends the captured page data to the local archiving server.
 */
export function sendToServer(captureData: CaptureData): Promise<ArchiveResponse> {
  const jsonData = JSON.stringify(captureData);

  let data: string;
  let headers: Record<string, string> = {
    'Content-Type': 'application/json'
  };

  // Compress if enabled (recommended for remote deployments)
  if (CONFIG.ENABLE_COMPRESSION) {
    const { compressed, originalSize, compressedSize } = compressData(jsonData);
    const compressionRatio = ((1 - compressedSize / originalSize) * 100).toFixed(1);
    console.log(`[Wayback] >>> Sending to server (${originalSize} bytes → ${compressedSize} bytes, ${compressionRatio}% reduction)...`);
    data = compressed;
    headers['Content-Encoding'] = 'gzip';
  } else {
    console.log(`[Wayback] >>> Sending to server (${jsonData.length} bytes, uncompressed)...`);
    data = jsonData;
  }

  // Add Basic Auth header if password is configured
  if (CONFIG.AUTH_PASSWORD) {
    const credentials = btoa(`wayback:${CONFIG.AUTH_PASSWORD}`);
    headers['Authorization'] = `Basic ${credentials}`;
  }

  return new Promise((resolve, reject) => {
    GM_xmlhttpRequest({
      method: 'POST',
      url: CONFIG.SERVER_URL,
      headers,
      data: data,
      binary: CONFIG.ENABLE_COMPRESSION,
      timeout: CONFIG.REQUEST_TIMEOUT,
      onload: (response) => {
        if (response.status === 200) {
          try {
            const result: ArchiveResponse = JSON.parse(response.responseText);
            console.log('[Wayback] ✓ Archived:', result.action);
            resolve(result);
          } catch (err) {
            console.error('[Wayback] ✗ Invalid JSON response:', response.responseText);
            reject(new Error('Invalid JSON response from server'));
          }
        } else {
          console.error('[Wayback] ✗ Failed:', response.status);
          reject(new Error(`Archive failed: ${response.status}`));
        }
      },
      onerror: (error) => {
        console.error('[Wayback] ✗ Error:', error);
        reject(error);
      },
      ontimeout: () => {
        console.error('[Wayback] ✗ Request timed out');
        reject(new Error('Archive request timed out'));
      }
    });
  });
}

/**
 * Sends an update request for an existing page.
 */
export function updateOnServer(pageId: number, captureData: CaptureData): Promise<ArchiveResponse> {
  const jsonData = JSON.stringify(captureData);
  const startTime = Date.now();

  let data: string;
  let headers: Record<string, string> = {
    'Content-Type': 'application/json'
  };

  // Compress if enabled (recommended for remote deployments)
  if (CONFIG.ENABLE_COMPRESSION) {
    const { compressed, originalSize, compressedSize } = compressData(jsonData);
    const compressionRatio = ((1 - compressedSize / originalSize) * 100).toFixed(1);
    console.log(`[Wayback] >>> Updating page ${pageId} on server (${originalSize} bytes → ${compressedSize} bytes, ${compressionRatio}% reduction)...`);
    data = compressed;
    headers['Content-Encoding'] = 'gzip';
  } else {
    console.log(`[Wayback] >>> Updating page ${pageId} on server (${jsonData.length} bytes, uncompressed)...`);
    data = jsonData;
  }

  // Add Basic Auth header if password is configured
  if (CONFIG.AUTH_PASSWORD) {
    const credentials = btoa(`wayback:${CONFIG.AUTH_PASSWORD}`);
    headers['Authorization'] = `Basic ${credentials}`;
  }

  return new Promise((resolve, reject) => {
    GM_xmlhttpRequest({
      method: 'PUT',
      url: `${CONFIG.SERVER_URL}/${pageId}`,
      headers,
      data: data,
      binary: CONFIG.ENABLE_COMPRESSION,
      timeout: CONFIG.REQUEST_TIMEOUT,
      onload: (response) => {
        const elapsed = Date.now() - startTime;
        if (response.status === 200) {
          try {
            const result: ArchiveResponse = JSON.parse(response.responseText);
            console.log(`[Wayback] ✓ Updated: ${result.action} (took ${elapsed}ms)`);
            resolve(result);
          } catch (err) {
            console.error(`[Wayback] ✗ Invalid JSON response (took ${elapsed}ms):`, response.responseText);
            reject(new Error('Invalid JSON response from server'));
          }
        } else {
          console.error(`[Wayback] ✗ Update failed: ${response.status} (took ${elapsed}ms)`);
          reject(new Error(`Update failed: ${response.status}`));
        }
      },
      onerror: (error) => {
        const elapsed = Date.now() - startTime;
        console.error(`[Wayback] ✗ Update error (took ${elapsed}ms):`, error);
        reject(error);
      },
      ontimeout: () => {
        const elapsed = Date.now() - startTime;
        console.error(`[Wayback] ✗ Update timed out after ${elapsed}ms`);
        reject(new Error('Update request timed out'));
      }
    });
  });
}
