const puppeteer = require('puppeteer');

const SERVER = 'http://localhost:8080';
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

async function createArchive(payload) {
  const response = await fetch(`${SERVER}/api/archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    throw new Error(`archive create failed: ${response.status} ${await response.text()}`);
  }

  return response.json();
}

async function deletePage(pageId) {
  await fetch(`${SERVER}/api/pages/${pageId}`, { method: 'DELETE' });
}

async function main() {
  const runId = Date.now();
  const title = `Stored XSS POC ${runId}`;
  let pageId = null;

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  try {
    const createResponse = await createArchive({
      url: "');window.__waybackStoredPoc=1;//",
      title,
      html: '<!doctype html><html><body>stored xss poc</body></html>',
    });
    pageId = createResponse.page_id;

    const home = await browser.newPage();
    await home.goto(`${SERVER}/`, { waitUntil: 'networkidle2', timeout: 120000 });
    await home.evaluate(() => {
      window.__waybackStoredPoc = 0;
      window.__lastOpenedURL = null;
      window.open = (url) => {
        window.__lastOpenedURL = url;
        return null;
      };
    });

    await home.click('#searchInput', { clickCount: 3 });
    await home.type('#searchInput', title);
    await home.waitForFunction((expectedTitle) => {
      return [...document.querySelectorAll('.page-item .page-title')].some((el) => el.textContent === expectedTitle);
    }, { timeout: 10000 }, title);

    await home.evaluate((expectedTitle) => {
      const item = [...document.querySelectorAll('.page-item')].find((el) => el.querySelector('.page-title')?.textContent === expectedTitle);
      if (!item) {
        throw new Error('matching page item not found');
      }

      const button = item.querySelector('.timeline-btn');
      if (!button) {
        throw new Error('timeline button not found');
      }

      button.click();
    }, title);

    const storedResult = await home.evaluate(() => ({
      poc: window.__waybackStoredPoc || 0,
      openedURL: window.__lastOpenedURL || '',
    }));

    if (storedResult.poc !== 0) {
      throw new Error(`stored XSS executed on home page: ${storedResult.poc}`);
    }
    if (!storedResult.openedURL.startsWith('/timeline?url=')) {
      throw new Error(`timeline button did not open a timeline URL: ${storedResult.openedURL}`);
    }

    const reflected = 'foo" onclick="window.__waybackReflectedPoc=1';
    const timeline = await browser.newPage();
    await timeline.goto(`${SERVER}/timeline?url=${encodeURIComponent(reflected)}`, { waitUntil: 'networkidle2', timeout: 120000 });
    await timeline.evaluate(() => {
      window.__waybackReflectedPoc = 0;
      const link = document.querySelector('#targetUrl a');
      if (!link) {
        throw new Error('target link not found');
      }
      link.removeAttribute('target');
      link.addEventListener('click', (event) => event.preventDefault());
    });
    await timeline.click('#targetUrl a');

    const reflectedResult = await timeline.evaluate(() => window.__waybackReflectedPoc || 0);
    if (reflectedResult !== 0) {
      throw new Error(`reflected XSS executed on timeline page: ${reflectedResult}`);
    }

    await timeline.close();
    await home.close();
    console.log('PASS test_web_ui_xss');
  } finally {
    if (pageId !== null) {
      await deletePage(pageId);
    }
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
