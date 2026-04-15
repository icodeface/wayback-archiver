const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

async function preparePage(browser, scriptContent, mode, url) {
  const page = await browser.newPage();
  page.on('console', (msg) => {
    const text = msg.text();
    if (text.includes('[Wayback]')) {
      console.log('[browser]', text);
    }
  });

  await page.evaluateOnNewDocument((testMode) => {
    window.__waybackTestMode = testMode;
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

      const jsonResponse = (pageId, action) => ({
        status: 200,
        responseText: JSON.stringify({ status: 'success', page_id: pageId, action }),
      });

      if (testMode === 'retry') {
        if (options.method === 'POST') {
          const attempts = window.__gmCalls.filter((call) => call.method === 'POST').length;
          if (attempts === 1) {
            window.__gmEvents.push('post-error');
            reply(options.onerror, { message: 'simulated failure' });
            return;
          }

          window.__gmEvents.push('post-success');
          reply(options.onload, jsonResponse(101, 'created'));
          return;
        }

        window.__gmEvents.push(`unexpected-${options.method}`);
        reply(options.onerror, { message: `unexpected method ${options.method}` });
        return;
      }

      if (testMode === 'unchanged-monitor') {
        if (options.method === 'POST') {
          window.__gmEvents.push('post-success');
          reply(options.onload, jsonResponse(202, 'unchanged'));
          return;
        }
        if (options.method === 'PUT') {
          window.__gmEvents.push('put-success');
          reply(options.onload, jsonResponse(202, 'updated'));
          return;
        }

        window.__gmEvents.push(`unexpected-${options.method}`);
        reply(options.onerror, { message: `unexpected method ${options.method}` });
      }
    };
  }, mode);

  await page.goto(url, { waitUntil: 'networkidle2', timeout: 120000 });
  await page.addScriptTag({ content: scriptContent });
  return page;
}

async function main() {
  const scriptPath = path.join(__dirname, '../../browser/dist/wayback-userscript.js');
  const scriptContent = fs.readFileSync(scriptPath, 'utf8');

  const fixtureServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><head><title>Wayback Userscript State</title></head><body><div id="mutations"></div><script>setTimeout(() => { const marker = document.createElement('div'); marker.id = 'late-mutation'; marker.textContent = 'late mutation'; document.body.appendChild(marker); }, 4000);</script></body></html>`);
  });

  await new Promise((resolve) => fixtureServer.listen(8093, '127.0.0.1', resolve));

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  try {
    const retryPage = await preparePage(browser, scriptContent, 'retry', 'http://lvh.me:8093/retry');
    await retryPage.waitForFunction(() => window.__gmEvents.includes('post-error'), { timeout: 15000 });
    await retryPage.evaluate(() => {
      window.dispatchEvent(new Event('beforeunload'));
    });
    await retryPage.waitForFunction(() => {
      return window.__gmCalls.filter((call) => call.method === 'POST').length >= 2;
    }, { timeout: 15000 });

    const retryAttempts = await retryPage.evaluate(() => window.__gmCalls.filter((call) => call.method === 'POST').length);
    if (retryAttempts < 2) {
      throw new Error(`expected retry after failed send, got ${retryAttempts} POST attempts`);
    }
    await retryPage.close();

    const monitorPage = await preparePage(browser, scriptContent, 'unchanged-monitor', 'http://lvh.me:8093/unchanged');
    await monitorPage.waitForFunction(() => window.__gmEvents.includes('post-success'), { timeout: 15000 });
    await monitorPage.evaluate(async () => {
      const root = document.getElementById('mutations');
      for (let i = 0; i < 12; i++) {
        const node = document.createElement('div');
        node.textContent = `mutation-${i}`;
        root.appendChild(node);
        await new Promise((resolve) => window.setTimeout(resolve, 0));
      }
    });
    await monitorPage.waitForFunction(() => {
      return window.__gmCalls.some((call) => call.method === 'PUT');
    }, { timeout: 15000 });

    const putAttempts = await monitorPage.evaluate(() => window.__gmCalls.filter((call) => call.method === 'PUT').length);
    if (putAttempts < 1) {
      throw new Error(`expected DOM monitor update after unchanged POST, got ${putAttempts} PUT attempts`);
    }
    await monitorPage.close();

    console.log('PASS test_userscript_state');
  } finally {
    await browser.close();
    await new Promise((resolve) => fixtureServer.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
