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

const distDir = path.join(__dirname, 'dist');
const outputPath = path.join(distDir, 'wayback.user.js');

// Module files to bundle in dependency order
const moduleFiles = [
  'config.js',
  'types.js',
  'page-filter.js',
  'content-fetcher.js',
  'css-parser.js',
  'resource-collector.js',
  'page-freezer.js',
  'style-inliner.js',
  'dom-collector.js',
  'archiver.js',
  'main.js',
];

// Tampermonkey header
const header = `// ==UserScript==
// @name         Wayback Web Archiver
// @namespace    http://tampermonkey.net/
// @version      1.0.0
// @description  Archive web pages to local server
// @author       You
// @match        *://*/*
// @grant        GM_xmlhttpRequest
// @grant        GM_cookie
// @connect      *
// @run-at       document-idle
// ==/UserScript==

(function() {
'use strict';

`;

const footer = `
})();
`;

// Read and process each module file
let bundledContent = '';

for (const moduleFile of moduleFiles) {
  const filePath = path.join(distDir, moduleFile);
  if (!fs.existsSync(filePath)) {
    console.warn(`Warning: Module file not found: ${moduleFile}`);
    continue;
  }

  let content = fs.readFileSync(filePath, 'utf8');

  // Remove TypeScript/ES module imports and exports
  content = content.replace(/^import\s+.*?from\s+['"][^'"]+['"];?\s*$/gm, '');
  content = content.replace(/^export\s+\{[^}]*\};?\s*$/gm, '');
  content = content.replace(/^export\s+(default\s+)?/gm, '');
  content = content.replace(/^"use strict";\s*/gm, '');
  content = content.replace(/^Object\.defineProperty\(exports,\s*"__esModule",\s*\{[^}]*\}\);\s*/gm, '');
  content = content.replace(/^exports\.\w+\s*=\s*\w+;\s*$/gm, '');

  // Remove Tampermonkey header if present
  content = content.replace(/^\/\/ ==UserScript==[\s\S]*?\/\/ ==\/UserScript==\s*/m, '');

  bundledContent += `// === ${moduleFile} ===\n${content.trim()}\n\n`;
}

const finalContent = header + bundledContent + footer;

fs.writeFileSync(outputPath, finalContent, 'utf8');
console.log('\n✓ Build complete: wayback.user.js');
console.log(`✓ Bundle size: ${finalContent.length} bytes`);
