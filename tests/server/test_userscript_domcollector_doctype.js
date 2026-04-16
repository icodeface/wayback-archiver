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

      if (typeof options.onload === 'function') {
        window.setTimeout(() => {
          window.__gmEvents.push(`${options.method.toLowerCase()}-success`);
          options.onload({
            status: 200,
            responseText: JSON.stringify({ status: 'success', page_id: 404, action: 'created' }),
          });
        }, 0);
      }
    };
  });

  await page.goto(url, { waitUntil: 'networkidle2', timeout: 120000 });
  await page.addScriptTag({ content: scriptContent });
  return page;
}

async function main() {
  const scriptPath = path.join(__dirname, '../../browser/dist/wayback-userscript.js');
  const scriptContent = fs.readFileSync(scriptPath, 'utf8');
  const targetURL = 'http://lvh.me:8095/domcollector-doctype';

  const fixtureServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end('<!doctype html><html><head><title>DOMCollector Doctype</title></head><body><main id="feed"></main></body></html>');
  });

  await new Promise((resolve) => fixtureServer.listen(8095, '127.0.0.1', resolve));

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  try {
    const page = await preparePage(browser, scriptContent, targetURL);

    await page.evaluate(() => {
      const bigText = 'archived content '.repeat(200);
      const node = document.createElement('article');
      node.id = 'removed-card';
      node.textContent = bigText;
      document.getElementById('feed').appendChild(node);

      window.setTimeout(() => {
        node.remove();
      }, 100);
    });

    await page.waitForFunction(() => window.__gmEvents.includes('post-success'), { timeout: 15000 });

    const payload = await page.evaluate(() => {
      const postCall = window.__gmCalls.find((call) => call.method === 'POST');
      if (!postCall) {
        throw new Error('archive request was not captured');
      }
      return JSON.parse(postCall.data);
    });

    if (!payload.html.startsWith('<!DOCTYPE html>')) {
      throw new Error(`expected archived HTML to preserve doctype, got: ${payload.html.slice(0, 80)}`);
    }

    if (!payload.html.includes('removed-card')) {
      throw new Error('expected merged HTML to contain removed virtual-scroll node');
    }

    await page.close();
    console.log('PASS test_userscript_domcollector_doctype');
  } finally {
    await browser.close();
    await new Promise((resolve) => fixtureServer.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
