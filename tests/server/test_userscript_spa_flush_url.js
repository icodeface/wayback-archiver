const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

async function preparePage(browser, scriptContent, url) {
  const page = await browser.newPage();
  page.on('console', (msg) => {
    const text = msg.text();
    if (text.includes('[Wayback]')) {
      console.log('[browser]', text);
    }
  });

  await page.evaluateOnNewDocument(() => {
    try {
      Object.defineProperty(window, 'navigation', {
        configurable: true,
        value: undefined,
      });
    } catch {
      // Fall back to the browser's Navigation API if it cannot be redefined.
    }

    window.__gmCalls = [];
    window.__gmEvents = [];

    window.GM_cookie = {
      list(_options, callback) {
        callback([]);
      },
    };

    window.GM_xmlhttpRequest = (options) => {
      window.__gmCalls.push({
        method: options.method,
        url: options.url,
        data: typeof options.data === 'string' ? options.data : '',
      });

      const reply = (handler, payload) => {
        if (typeof handler === 'function') {
          window.setTimeout(() => handler(payload), 0);
        }
      };

      if (options.method === 'POST') {
        window.__gmEvents.push('post-success');
        reply(options.onload, {
          status: 200,
          responseText: JSON.stringify({ status: 'success', page_id: 303, action: 'created' }),
        });
        return;
      }

      if (options.method === 'PUT') {
        window.__gmEvents.push('put-success');
        reply(options.onload, {
          status: 200,
          responseText: JSON.stringify({ status: 'success', page_id: 303, action: 'updated' }),
        });
        return;
      }

      window.__gmEvents.push(`unexpected-${options.method}`);
      reply(options.onerror, { message: `unexpected method ${options.method}` });
    };
  });

  await page.goto(url, { waitUntil: 'networkidle2', timeout: 120000 });
  await page.addScriptTag({ content: scriptContent });
  return page;
}

async function main() {
  const scriptPath = path.join(__dirname, '../../browser/dist/wayback-userscript.js');
  const scriptContent = fs.readFileSync(scriptPath, 'utf8');
  const oldURL = 'http://lvh.me:8094/old-route';
  const newPath = '/new-route';

  const fixtureServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><head><title>Old Title</title></head><body><main id="old-marker">old page body</main></body></html>`);
  });

  await new Promise((resolve) => fixtureServer.listen(8094, '127.0.0.1', resolve));

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  try {
    const page = await preparePage(browser, scriptContent, oldURL);
    await page.waitForFunction(() => window.__gmEvents.includes('post-success'), { timeout: 15000 });

    await page.evaluate((pathAfterPush) => {
      history.pushState({}, '', pathAfterPush);
      document.title = 'New Title';
      window.setTimeout(() => {
        document.body.innerHTML = '<main id="new-marker">new page body</main>';
      }, 200);
    }, newPath);

    await page.waitForFunction(() => window.__gmEvents.includes('put-success'), { timeout: 15000 });

    const putPayload = await page.evaluate(() => {
      const putCall = window.__gmCalls.find((call) => call.method === 'PUT');
      if (!putCall) {
        throw new Error('update request was not captured');
      }
      return JSON.parse(putCall.data);
    });

    if (putPayload.url !== oldURL) {
      throw new Error(`expected flush PUT to keep old URL ${oldURL}, got ${putPayload.url}`);
    }

    if (putPayload.title !== 'Old Title') {
      throw new Error(`expected flush PUT to keep old title Old Title, got ${putPayload.title}`);
    }

    if (!putPayload.html.includes('old-marker')) {
      throw new Error(`expected flush PUT HTML to contain old DOM, got ${putPayload.html}`);
    }

    if (putPayload.html.includes('new-marker')) {
      throw new Error(`flush PUT HTML unexpectedly captured new DOM: ${putPayload.html}`);
    }

    await page.close();
    console.log('PASS test_userscript_spa_flush_url');
  } finally {
    await browser.close();
    await new Promise((resolve) => fixtureServer.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
