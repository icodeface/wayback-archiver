const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const ARCHIVE_ENDPOINT = 'http://localhost:8080/api/archive';

async function main() {
  const bundlePath = path.join(__dirname, '../../browser/dist/wayback-puppeteer.js');
  const bundle = fs.readFileSync(bundlePath, 'utf8');
  const repeatedText = 'duplicate-node-content '.repeat(180);

  const server = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><head><title>Duplicate Collector</title></head><body><main id="feed"><article class="dup-item"><h2>duplicate sentinel</h2><p>${repeatedText}</p></article><article class="dup-item"><h2>duplicate sentinel</h2><p>${repeatedText}</p></article></main></body></html>`);
  });

  await new Promise((resolve) => server.listen(8104, '127.0.0.1', resolve));

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
    await page.goto('http://lvh.me:8104/', { waitUntil: 'networkidle2', timeout: 120000 });
    await page.waitForFunction(() => document.querySelectorAll('.dup-item').length === 2, { timeout: 10000 });

    await page.evaluate(async () => {
      const items = Array.from(document.querySelectorAll('.dup-item'));
      const archivePromise = window.archivePage();
      setTimeout(() => {
        for (const item of items) {
          item.remove();
        }
      }, 0);
      await archivePromise;
    });

    const capture = await page.evaluate(() => {
      if (!window.__archiveRequests.length) {
        throw new Error('archive request was not captured');
      }
      return JSON.parse(window.__archiveRequests[0].body);
    });

    const duplicateMatches = capture.html.match(/<article class="dup-item">/g) || [];
    if (duplicateMatches.length !== 1) {
      throw new Error(`expected identical duplicate nodes to collapse to one archived copy, got ${duplicateMatches.length}\n${capture.html}`);
    }

    await page.close();
    console.log('PASS test_userscript_domcollector_duplicate');
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
