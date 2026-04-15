const fs = require('fs');
const path = require('path');
const vm = require('vm');

function evaluateUserscript(url) {
  const userscriptPath = path.join(__dirname, '../../browser/dist/wayback-userscript.js');
  const source = fs.readFileSync(userscriptPath, 'utf8');
  const parsedURL = new URL(url);
  const logs = [];

  const window = {
    location: {
      href: parsedURL.href,
      hostname: parsedURL.hostname,
      origin: parsedURL.origin,
    },
    self: null,
    top: null,
    scrollY: 0,
    setTimeout: () => 1,
    clearTimeout: () => {},
    setInterval: () => 1,
    clearInterval: () => {},
    addEventListener: () => {},
  };
  window.self = window;
  window.top = window;

  const sandbox = {
    URL,
    window,
    location: window.location,
    history: {
      pushState: () => {},
      replaceState: () => {},
    },
    document: {
      title: 'Test Page',
      body: {},
      visibilityState: 'visible',
      addEventListener: () => {},
      querySelectorAll: () => [],
    },
    navigator: { userAgent: 'node-test' },
    MutationObserver: class {
      observe() {}
      disconnect() {}
    },
    console: {
      log: (...args) => logs.push(args.join(' ')),
      warn: (...args) => logs.push(args.join(' ')),
      error: (...args) => logs.push(args.join(' ')),
    },
    GM_xmlhttpRequest: () => {
      throw new Error('userscript should not send network requests during skip test');
    },
    setTimeout: window.setTimeout,
    clearTimeout: window.clearTimeout,
    setInterval: window.setInterval,
    clearInterval: window.clearInterval,
  };

  vm.runInNewContext(source, sandbox, { filename: userscriptPath });
  return logs;
}

async function main() {
  const { shouldSkipURL } = await import('../../browser/dist/page-filter.js');

  const cases = [
    ['localhost', 'http://localhost:8080/', true],
    ['loopback ipv4', 'http://127.0.0.1/', true],
    ['private 10/8', 'http://10.1.2.3/', true],
    ['private 172.16/12', 'http://172.16.5.4/', true],
    ['private 192.168/16', 'http://192.168.1.9/', true],
    ['link-local ipv4', 'http://169.254.10.20/', true],
    ['loopback ipv6', 'http://[::1]/', true],
    ['unique-local ipv6', 'http://[fd00::1234]/', true],
    ['link-local ipv6', 'http://[fe80::abcd]/', true],
    ['link-local ipv6 fe90', 'http://[fe90::1]/', true],
    ['link-local ipv6 fea0', 'http://[fea0::1]/', true],
    ['link-local ipv6 febf', 'http://[febf::1]/', true],
    ['ipv4-mapped local ipv6', 'http://[::ffff:192.168.1.9]/', true],
    ['.local hostname', 'http://printer.local/', true],
    ['public domain starting fd', 'https://fd.example.com/', false],
    ['public domain starting fc', 'https://fc.example.com/', false],
    ['file url', 'file:///tmp/test.html', true],
    ['browser internal', 'chrome://extensions/', true],
    ['public ipv4 boundary allowed', 'http://172.15.255.255/', false],
    ['public ipv6 allowed', 'http://[2606:4700:4700::1111]/', false],
    ['public domain allowed', 'https://example.com/', false],
  ];

  let failed = 0;
  for (const [name, url, expected] of cases) {
    const actual = shouldSkipURL(url);
    if (actual !== expected) {
      failed++;
      console.error(`FAIL ${name}: shouldSkipURL(${url}) = ${actual}, want ${expected}`);
    } else {
      console.log(`PASS ${name}`);
    }
  }

  if (failed > 0) {
    process.exit(1);
  }

  const bundledCases = [
    ['bundled localhost', 'http://localhost:8080/'],
    ['bundled private ipv4', 'http://192.168.1.9/'],
  ];

  for (const [name, url] of bundledCases) {
    const logs = evaluateUserscript(url);
    const skipped = logs.some((line) => line.includes('[Wayback] Skipping page:'));
    const loaded = logs.some((line) => line.includes('[Wayback] Script loaded for:'));

    if (!skipped || loaded) {
      failed++;
      console.error(`FAIL ${name}: bundled userscript logs = ${JSON.stringify(logs)}`);
    } else {
      console.log(`PASS ${name}`);
    }
  }

  if (failed > 0) {
    process.exit(1);
  }

  console.log('PASS test_page_filter');
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
