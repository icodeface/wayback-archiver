const puppeteer = require('puppeteer');
const path = require('path');

async function testPuppeteerScript() {
  console.log('Starting Puppeteer test...');

  const browser = await puppeteer.launch({
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    headless: false,
    args: ['--disable-web-security', '--disable-features=IsolateOrigins,site-per-process'],
  });

  const page = await browser.newPage();

  // Listen to console messages
  page.on('console', msg => console.log('PAGE LOG:', msg.text()));

  // Navigate to a test page first
  console.log('Navigating to baidu.com...');
  await page.goto('https://www.baidu.com', { waitUntil: 'networkidle0' });

  // Load the script after navigation
  const scriptPath = path.join(__dirname, '../../browser/dist/wayback-puppeteer.js');
  console.log('Loading script:', scriptPath);
  await page.addScriptTag({ path: scriptPath });

  // Check if function is available
  const hasFunction = await page.evaluate(() => typeof window.archivePage);
  console.log('window.archivePage type:', hasFunction);

  // Call archivePage
  console.log('Calling archivePage()...');
  const result = await page.evaluate(async () => {
    console.log('window.archivePage:', typeof window.archivePage);
    console.log('window keys:', Object.keys(window).filter(k => k.includes('archive')));
    try {
      await window.archivePage();
      return { success: true };
    } catch (error) {
      return { success: false, error: error.message, stack: error.stack };
    }
  });

  console.log('Result:', result);

  await browser.close();
}

testPuppeteerScript().catch(console.error);
