const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

// Step 1: Compile TypeScript
console.log('Compiling TypeScript...');
try {
  execSync('npx tsc', { cwd: __dirname, stdio: 'inherit' });
} catch (error) {
  console.error('TypeScript compilation failed');
  process.exit(1);
}

// Version from git tag or environment variable
const version = process.env.VERSION
  || execSync('git describe --tags --always --dirty 2>/dev/null || echo "dev"', { encoding: 'utf8' }).trim().replace(/^v/, '');

const distDir = path.join(__dirname, 'dist');
const userscriptPath = path.join(distDir, 'wayback-userscript.js');
const puppeteerPath = path.join(distDir, 'wayback-puppeteer.js');
const pakoPath = path.join(__dirname, 'node_modules/pako/dist/pako.min.js');

// Module files to bundle in dependency order
const userscriptModules = [
  'config.js',
  'types.js',
  'page-filter.js',
  'content-fetcher.js',
  'css-parser.js',
  'resource-collector.js',
  'page-freezer.js',
  'style-inliner.js',
  'dom-collector.js',
  'html-url-normalizer.js',
  'bridge-auth.js',
  'spa-coordinator.js',
  'frame-capture.js',
  'archiver.js',
  'main.js',
];

const puppeteerModules = [
  'config.js',
  'types.js',
  'page-filter.js',
  'page-freezer.js',
  'style-inliner.js',
  'dom-collector.js',
  'html-url-normalizer.js',
  'bridge-auth.js',
  'frame-capture.js',
  'puppeteer.js',
];

// Tampermonkey header
const header = `// ==UserScript==
// @name         Wayback Web Archiver
// @namespace    http://tampermonkey.net/
// @version      ${version}
// @description  Archive web pages to local server
// @author       You
// @match        *://*/*
// @grant        GM_xmlhttpRequest
// @grant        GM_cookie
// @grant        GM_getValue
// @grant        GM_setValue
// @grant        GM_addValueChangeListener
// @connect      localhost
// @connect      *
// @require      https://cdnjs.cloudflare.com/ajax/libs/pako/2.1.0/pako.min.js
// @run-at       document-idle
// ==/UserScript==

(function() {
'use strict';

`;

const footer = `
})();
`;

function bundleModules(moduleFiles) {
  let bundledContent = '';
  for (const moduleFile of moduleFiles) {
    const filePath = path.join(distDir, moduleFile);
    if (!fs.existsSync(filePath)) {
      console.warn(`Warning: Module file not found: ${moduleFile}`);
      continue;
    }
    let content = fs.readFileSync(filePath, 'utf8');
    content = content.replace(/^import\s+.*?from\s+['"][^'"]+['"];?\s*$/gm, '');
    content = content.replace(/^export\s+\{[^}]*\};?\s*$/gm, '');
    content = content.replace(/^export\s+(default\s+)?/gm, '');
    content = content.replace(/^"use strict";\s*/gm, '');
    content = content.replace(/^Object\.defineProperty\(exports,\s*"__esModule",\s*\{[^}]*\}\);\s*/gm, '');
    content = content.replace(/^exports\.\w+\s*=\s*\w+;\s*$/gm, '');
    content = content.replace(/^\/\/ ==UserScript==[\s\S]*?\/\/ ==\/UserScript==\s*/m, '');
    bundledContent += `// === ${moduleFile} ===\n${content.trim()}\n\n`;
  }
  return bundledContent;
}

// Build userscript
const userscriptContent = header + bundleModules(userscriptModules) + footer;
fs.writeFileSync(userscriptPath, userscriptContent, 'utf8');
console.log('\n✓ Build complete: wayback-userscript.js');
console.log(`✓ Bundle size: ${userscriptContent.length} bytes`);

// Build Puppeteer bundle with pako embedded
const pakoContent = fs.readFileSync(pakoPath, 'utf8');
const puppeteerContent = `(function() {
'use strict';

// === pako.min.js ===
${pakoContent}

${bundleModules(puppeteerModules)}
})();
`;
fs.writeFileSync(puppeteerPath, puppeteerContent, 'utf8');
console.log('✓ Build complete: wayback-puppeteer.js');
console.log(`✓ Bundle size: ${puppeteerContent.length} bytes`);
