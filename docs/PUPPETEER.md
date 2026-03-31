# Puppeteer Integration

Use `wayback-puppeteer.js` for automated web archiving with Puppeteer.

## Quick Start

```javascript
const puppeteer = require('puppeteer');

const browser = await puppeteer.launch({
  args: ['--disable-web-security'] // Required for localhost CORS
});
const page = await browser.newPage();

// Navigate to target page
await page.goto('https://example.com', { waitUntil: 'networkidle0' });

// Load archiver script
await page.addScriptTag({
  path: 'browser/dist/wayback-puppeteer.js'
});

// Archive the page
await page.evaluate(() => window.archivePage());

await browser.close();
```

## Build

```bash
cd browser && npm run build
```

Generates:
- `dist/wayback-userscript.js` - Tampermonkey userscript
- `dist/wayback-puppeteer.js` - Puppeteer bundle (includes pako)

## Features

- Standalone bundle with embedded pako compression
- Same capture quality as Tampermonkey version
- DOM stability detection
- CSSOM serialization
- Layout styles inlining
- Virtual scroll node collection

## Configuration

Edit `browser/src/config.ts` before building:

```typescript
export const CONFIG = {
  SERVER_URL: 'http://localhost:8080/api/archive',
  ENABLE_COMPRESSION: false, // Set true for remote server
  AUTH_PASSWORD: '', // Set if server requires auth
};
```

## CORS Handling

Puppeteer pages cannot access `localhost` by default due to CORS. Solutions:

### Option 1: Disable Web Security (Testing)
```javascript
const browser = await puppeteer.launch({
  args: ['--disable-web-security', '--disable-features=IsolateOrigins,site-per-process']
});
```

### Option 2: Use Local HTML Files
```javascript
await page.goto('file:///path/to/page.html');
```

### Option 3: Configure Server CORS
Add CORS headers to the server for production use.

## Example: Batch Archiving

```javascript
const urls = [
  'https://example.com',
  'https://www.baidu.com',
  'https://github.com'
];

for (const url of urls) {
  await page.goto(url, { waitUntil: 'networkidle0' });
  await page.evaluate(() => window.archivePage());
  console.log(`Archived: ${url}`);
}
```
