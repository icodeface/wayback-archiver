const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

async function main() {
  const bundlePath = path.join(__dirname, '../../browser/dist/wayback-puppeteer.js');
  const bundle = fs.readFileSync(bundlePath, 'utf8');
  const runId = Date.now();
  const fixtureHost = 'lvh.me';
  const parentURL = `http://${fixtureHost}:8091/parent?run=${runId}`;
  const publicCSSURL = 'https://cdnjs.cloudflare.com/ajax/libs/normalize/8.0.1/normalize.min.css';

  const fixtureServer = http.createServer((req, res) => {
    if (req.url.startsWith('/parent')) {
      res.writeHead(200, { 'Content-Type': 'text/html' });
      res.end(`<!doctype html><html><head><title>Frame Parent</title></head><body><h1>Parent</h1><iframe id="child-frame" src="/child?run=${runId}"></iframe></body></html>`);
      return;
    }

    if (req.url.startsWith('/child')) {
      res.writeHead(200, { 'Content-Type': 'text/html' });
      res.end(`<!doctype html><html><head><title>Child</title><link rel="stylesheet" href="${publicCSSURL}"></head><body><div class="msg">Frame body content</div></body></html>`);
      return;
    }

    res.writeHead(404);
    res.end('not found');
  });

  await new Promise((resolve) => fixtureServer.listen(8091, '127.0.0.1', resolve));

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    args: [
      '--no-sandbox',
      '--disable-web-security',
      '--disable-features=IsolateOrigins,site-per-process',
    ],
  });

  try {
    const page = await browser.newPage();
    page.on('console', (msg) => {
      console.log('[browser]', msg.type(), msg.text());
    });
    await page.evaluateOnNewDocument(bundle);
    await page.goto(parentURL, { waitUntil: 'networkidle2', timeout: 120000 });
    await page.evaluate(async () => {
      await window.archivePage();
    });
    await new Promise((resolve) => setTimeout(resolve, 1500));

    const pagesResponse = await fetch('http://127.0.0.1:8080/api/pages');
    const pagesPayload = await pagesResponse.json();
    const pages = Array.isArray(pagesPayload) ? pagesPayload : (pagesPayload.pages || []);
    const archived = pages.find((item) => item.url === parentURL);
    if (!archived) {
      throw new Error('Archived page not found');
    }

    const viewPage = await browser.newPage();
    const cssRequests = [];
    viewPage.on('request', (req) => {
      const url = req.url();
      if (req.resourceType() === 'stylesheet' || /\.css(\?|$)/i.test(url)) {
        cssRequests.push(url);
      }
    });

    await viewPage.goto(`http://127.0.0.1:8080/view/${archived.id}`, { waitUntil: 'networkidle2', timeout: 120000 });
    const result = await viewPage.evaluate(() => {
      const frame = document.querySelector('iframe#child-frame');
      const childDocument = frame && frame.contentDocument ? frame.contentDocument : null;
      return {
        iframe: frame ? {
          src: frame.getAttribute('src') || '',
          hasSrcdoc: frame.hasAttribute('srcdoc'),
        } : null,
        childText: childDocument ? (childDocument.body.innerText || '').trim() : '',
      };
    });

    const uniqueCSS = [...new Set(cssRequests)];
    const leakedCSS = uniqueCSS.filter((url) => url === publicCSSURL || url.includes(`${fixtureHost}:8091`) || url.includes('127.0.0.1:8091'));

    console.log(JSON.stringify({ archivedId: archived.id, css: uniqueCSS, result }, null, 2));

    if (leakedCSS.length > 0) {
      throw new Error(`Leaked iframe CSS requests: ${leakedCSS.join(', ')}`);
    }
    if (!result.iframe || result.iframe.hasSrcdoc || !result.iframe.src.startsWith('/archive/')) {
      throw new Error('Iframe was not rewritten to a local archive URL');
    }
    if (!result.childText.includes('Frame body content')) {
      throw new Error('Archived iframe content missing');
    }

    await viewPage.close();
    await page.close();
    console.log('PASS test_iframe_capture');
  } finally {
    await browser.close();
    await new Promise((resolve) => fixtureServer.close(resolve));
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
