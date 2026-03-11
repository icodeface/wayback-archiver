// Archiver - sends captured data to the server

import { CONFIG } from './config';
import { CaptureData, ArchiveResponse } from './types';

/**
 * Sends the captured page data to the local archiving server.
 */
export function sendToServer(captureData: CaptureData): Promise<ArchiveResponse> {
  console.log('[Wayback] >>> Sending to server...');

  const headers: Record<string, string> = {
    'Content-Type': 'application/json'
  };

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
      data: JSON.stringify(captureData),
      onload: (response) => {
        if (response.status === 200) {
          const result: ArchiveResponse = JSON.parse(response.responseText);
          console.log('[Wayback] ✓ Archived:', result.action);
          resolve(result);
        } else {
          console.error('[Wayback] ✗ Failed:', response.status);
          reject(new Error(`Archive failed: ${response.status}`));
        }
      },
      onerror: (error) => {
        console.error('[Wayback] ✗ Error:', error);
        reject(error);
      }
    });
  });
}

/**
 * Sends an update request for an existing page.
 */
export function updateOnServer(pageId: number, captureData: CaptureData): Promise<ArchiveResponse> {
  console.log('[Wayback] >>> Updating page', pageId, 'on server...');

  const headers: Record<string, string> = {
    'Content-Type': 'application/json'
  };

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
      data: JSON.stringify(captureData),
      onload: (response) => {
        if (response.status === 200) {
          const result: ArchiveResponse = JSON.parse(response.responseText);
          console.log('[Wayback] ✓ Updated:', result.action);
          resolve(result);
        } else {
          console.error('[Wayback] ✗ Update failed:', response.status);
          reject(new Error(`Update failed: ${response.status}`));
        }
      },
      onerror: (error) => {
        console.error('[Wayback] ✗ Update error:', error);
        reject(error);
      }
    });
  });
}
