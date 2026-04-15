const http = require('http');
const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer');

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

async function main() {
  const bundlePath = path.join(__dirname, '../../browser/dist/wayback-puppeteer.js');
  const bundle = fs.readFileSync(bundlePath, 'utf8');

  const parentServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><body><h1>Parent</h1><iframe id="victim-frame" src="http://lvh.me:8092/child"></iframe></body></html>`);
  });

  const childServer = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(`<!doctype html><html><head><title>Victim Child</title></head><body><div id="secret">cross-origin child content</div></body></html>`);
  });

  await new Promise((resolve) => parentServer.listen(8091, '127.0.0.1', resolve));
  await new Promise((resolve) => childServer.listen(8092, '127.0.0.1', resolve));

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  try {
    const page = await browser.newPage();
    await page.evaluateOnNewDocument(bundle);
    await page.goto('http://lvh.me:8091/', { waitUntil: 'networkidle2', timeout: 120000 });

    const result = await page.evaluate(() => new Promise((resolve) => {
      const frame = document.getElementById('victim-frame');
      if (!frame || !frame.contentWindow) {
        throw new Error('victim iframe not ready');
      }

      let publicMessages = 0;
      const timer = window.setTimeout(() => {
        window.removeEventListener('message', onMessage);
        resolve({ forgedAccepted: false, publicMessages });
      }, 4000);

      function onMessage(event) {
        if (event.data && event.data.source === 'wayback-frame-capture') {
          publicMessages += 1;
        }
      }

      window.addEventListener('message', onMessage);

      const channel = new MessageChannel();
      channel.port1.onmessage = (event) => {
        window.clearTimeout(timer);
        window.removeEventListener('message', onMessage);
        resolve({
          forgedAccepted: true,
          publicMessages,
          data: event.data,
        });
      };

      frame.contentWindow.postMessage({
        source: 'wayback-frame-capture',
        type: 'capture-frame',
        frameId: 'forged-frame-id',
        parentOrigin: window.location.origin,
        targetOrigin: new URL(frame.src, window.location.href).origin,
        requestId: 'attacker-request',
        timestamp: Date.now(),
        token: 'forged-token',
      }, '*', [channel.port2]);
    }));

    await page.close();

    if (result.forgedAccepted) {
      throw new Error(`forged frame bridge request unexpectedly succeeded: ${JSON.stringify(result.data)}`);
    }
    if (result.publicMessages !== 0) {
      throw new Error(`forged request triggered unexpected public bridge messages: ${result.publicMessages}`);
    }

    console.log('PASS test_frame_bridge_origin');
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
