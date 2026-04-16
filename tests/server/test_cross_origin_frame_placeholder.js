const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const ARCHIVE_ENDPOINT = 'http://localhost:8080/api/archive';

async function main() {
  const bundlePath = path.join(__dirname, '../../browser/dist/wayback-puppeteer.js');
  const bundle = fs.readFileSync(bundlePath, 'utf8');
  const childURL = 'http://lvh.me:8097/child';

  const parentServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><head><title>Parent Page</title></head><body><h1>Parent</h1><iframe id="cross-origin-frame" src="${childURL}"></iframe></body></html>`);
  });

  const childServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end('<!doctype html><html><head><title>Hidden Child</title></head><body style="display: none; visibility: hidden;"><main id="frame-marker">hidden placeholder body</main></body></html>');
  });

  await new Promise((resolve) => parentServer.listen(8096, '127.0.0.1', resolve));
  await new Promise((resolve) => childServer.listen(8097, '127.0.0.1', resolve));

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
    await page.goto('http://lvh.me:8096/', { waitUntil: 'networkidle2', timeout: 120000 });
    await page.waitForFunction(() => !!document.getElementById('cross-origin-frame'), { timeout: 10000 });

    await page.evaluate(() => window.archivePage());

    const capture = await page.evaluate(() => {
      if (!window.__archiveRequests.length) {
        throw new Error('archive request was not captured');
      }
      return JSON.parse(window.__archiveRequests[0].body);
    });

    const frame = Array.isArray(capture.frames)
      ? capture.frames.find((item) => item && item.url === 'http://lvh.me:8097/child')
      : null;

    if (frame) {
      throw new Error(`expected hidden cross-origin iframe to be skipped, got ${JSON.stringify(frame)}`);
    }

    if (!capture.html.includes('data-wayback-frame-status="placeholder"')) {
      throw new Error(`expected parent HTML to mark iframe as placeholder, got ${capture.html}`);
    }

    await page.close();
    console.log('PASS test_cross_origin_frame_placeholder');
  } finally {
    await browser.close();
    await new Promise((resolve) => parentServer.close(resolve));
    await new Promise((resolve) => childServer.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
