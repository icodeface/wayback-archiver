const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const ARCHIVE_ENDPOINT = 'http://localhost:8080/api/archive';

async function main() {
  const bundlePath = path.join(__dirname, '../../browser/dist/wayback-puppeteer.js');
  const bundle = fs.readFileSync(bundlePath, 'utf8');
  const emptyURL = 'http://lvh.me:8102/empty';
  const placeholderURL = 'http://lvh.me:8103/placeholder';

  const parentServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><head><title>Parent Page</title></head><body><iframe id="empty-frame" src="${emptyURL}"></iframe><iframe id="placeholder-frame" src="${placeholderURL}"></iframe></body></html>`);
  });

  const emptyServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end('<!doctype html><html><head><title>Empty Child</title></head><body></body></html>');
  });

  const placeholderServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end('<!doctype html><html><head><title>Placeholder Child</title></head><body style="display:none;visibility:hidden"><main>placeholder body</main></body></html>');
  });

  await new Promise((resolve) => parentServer.listen(8101, '127.0.0.1', resolve));
  await new Promise((resolve) => emptyServer.listen(8102, '127.0.0.1', resolve));
  await new Promise((resolve) => placeholderServer.listen(8103, '127.0.0.1', resolve));

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  try {
    const page = await browser.newPage();
    page.on('console', (msg) => {
      const text = msg.text();
      if (text.includes('[Wayback]')) {
        console.log('[browser]', text);
      }
    });

    await page.evaluateOnNewDocument((archiveEndpoint) => {
      window.__archiveRequests = [];
      const nativeFetch = window.fetch.bind(window);

      window.fetch = async (input, init) => {
        const url = typeof input === 'string'
          ? input
          : (typeof Request !== 'undefined' && input instanceof Request ? input.url : String(input));
        const method = init?.method || (typeof Request !== 'undefined' && input instanceof Request ? input.method : 'GET');

        if (url === archiveEndpoint && method === 'POST') {
          window.__archiveRequests.push({
            url,
            body: typeof init?.body === 'string' ? init.body : '',
          });

          return new Response(JSON.stringify({
            status: 'success',
            page_id: 1,
            action: 'created',
          }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          });
        }

        return nativeFetch(input, init);
      };
    }, ARCHIVE_ENDPOINT);

    await page.evaluateOnNewDocument(bundle);
    await page.goto('http://lvh.me:8101/', { waitUntil: 'networkidle2', timeout: 120000 });
    await page.evaluate(() => window.archivePage());

    const capture = await page.evaluate(() => {
      if (!window.__archiveRequests.length) {
        throw new Error('archive request was not captured');
      }
      return JSON.parse(window.__archiveRequests[0].body);
    });

    if (Array.isArray(capture.frames) && capture.frames.length !== 0) {
      throw new Error(`expected empty/placeholder frames to be skipped from frames payload, got ${JSON.stringify(capture.frames)}`);
    }

    const html = capture.html;
    const emptyTagMatch = html.match(/<iframe[^>]*id="empty-frame"[^>]*>/i);
    if (!emptyTagMatch) {
      throw new Error(`missing empty iframe tag in parent HTML: ${html}`);
    }
    if (!emptyTagMatch[0].includes('data-wayback-frame-status="empty"')) {
      throw new Error(`empty iframe should be marked as empty: ${emptyTagMatch[0]}`);
    }
    if (!emptyTagMatch[0].includes(`data-wayback-original-src="${emptyURL}"`)) {
      throw new Error(`empty iframe should preserve original src: ${emptyTagMatch[0]}`);
    }
    if (/\ssrc=|\ssrcdoc=/i.test(emptyTagMatch[0])) {
      throw new Error(`empty iframe should have src/srcdoc removed: ${emptyTagMatch[0]}`);
    }

    const placeholderTagMatch = html.match(/<iframe[^>]*id="placeholder-frame"[^>]*>/i);
    if (!placeholderTagMatch) {
      throw new Error(`missing placeholder iframe tag in parent HTML: ${html}`);
    }
    if (!placeholderTagMatch[0].includes('data-wayback-frame-status="placeholder"')) {
      throw new Error(`placeholder iframe should be marked as placeholder: ${placeholderTagMatch[0]}`);
    }
    if (!placeholderTagMatch[0].includes(`data-wayback-original-src="${placeholderURL}"`)) {
      throw new Error(`placeholder iframe should preserve original src: ${placeholderTagMatch[0]}`);
    }
    if (/\ssrc=|\ssrcdoc=/i.test(placeholderTagMatch[0])) {
      throw new Error(`placeholder iframe should have src/srcdoc removed: ${placeholderTagMatch[0]}`);
    }

    await page.close();
    console.log('PASS test_frame_capture_status_rewrite');
  } finally {
    await browser.close();
    await new Promise((resolve) => parentServer.close(resolve));
    await new Promise((resolve) => emptyServer.close(resolve));
    await new Promise((resolve) => placeholderServer.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
