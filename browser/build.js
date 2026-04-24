const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');
const ts = require('typescript');

// Version from git tag or environment variable
const version = process.env.VERSION
  || execSync('git describe --tags --always --dirty 2>/dev/null || echo "dev"', { encoding: 'utf8' }).trim().replace(/^v/, '');

const distDir = path.join(__dirname, 'dist');
const srcDir = path.join(__dirname, 'src');
const userscriptPath = path.join(distDir, 'wayback-userscript.js');
const puppeteerPath = path.join(distDir, 'wayback-puppeteer.js');
const distPackageJSONPath = path.join(distDir, 'package.json');
const pakoPath = path.join(__dirname, 'node_modules/pako/dist/pako.min.js');

function rewriteRelativeESMImports() {
  for (const fileName of fs.readdirSync(distDir)) {
    if (!fileName.endsWith('.js')) {
      continue;
    }

    const filePath = path.join(distDir, fileName);
    const content = fs.readFileSync(filePath, 'utf8');
    const rewritten = content.replace(
      /((?:import|export)\s+(?:[^'"]*?\s+from\s+)?)(['"])(\.{1,2}\/[^'"]+)(\2)/g,
      (match, prefix, quote, specifier, suffix) => {
        if (path.extname(specifier)) {
          return match;
        }
        return `${prefix}${quote}${specifier}.js${suffix}`;
      }
    );

    if (rewritten !== content) {
      fs.writeFileSync(filePath, rewritten, 'utf8');
    }
  }
}

// Step 1: Compile TypeScript
console.log('Compiling TypeScript...');
try {
  execSync('npx tsc', { cwd: __dirname, stdio: 'inherit' });
  rewriteRelativeESMImports();
} catch (error) {
  console.error('TypeScript compilation failed');
  process.exit(1);
}

// Module files to bundle in dependency order
const userscriptModules = [
  'config.js',
  'types.js',
  'page-filter.js',
  'page-freezer.js',
  'style-inliner.js',
  'dom-collector.js',
  'html-url-normalizer.js',
  'bridge-auth.js',
  'spa-coordinator.js',
  'frame-capture.js',
  'viewport-meta.js',
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
  'viewport-meta.js',
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

function resolveSourceModulePath(moduleFile) {
  const baseName = moduleFile.replace(/\.js$/, '');
  const candidates = [
    path.join(srcDir, `${baseName}.ts`),
    path.join(srcDir, `${baseName}.js`),
  ];

  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }

  throw new Error(`Source module not found for ${moduleFile}`);
}

function collectBindings(nameNode, out) {
  if (!nameNode) {
    return;
  }

  if (ts.isIdentifier(nameNode)) {
    out.push(nameNode.text);
    return;
  }

  if (ts.isArrayBindingPattern(nameNode) || ts.isObjectBindingPattern(nameNode)) {
    for (const element of nameNode.elements) {
      if (ts.isBindingElement(element)) {
        collectBindings(element.name, out);
      }
    }
  }
}

function topLevelRuntimeDeclarations(filePath) {
  const sourceText = fs.readFileSync(filePath, 'utf8');
  const sourceFile = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.ES2020, true, ts.ScriptKind.JS);
  const declarations = [];

  for (const statement of sourceFile.statements) {
    if (ts.isFunctionDeclaration(statement) && statement.name) {
      declarations.push({
        name: statement.name.text,
        filePath,
      });
      continue;
    }

    if (ts.isClassDeclaration(statement) && statement.name) {
      declarations.push({
        name: statement.name.text,
        filePath,
      });
      continue;
    }

    if (!ts.isVariableStatement(statement)) {
      continue;
    }

    for (const declaration of statement.declarationList.declarations) {
      const names = [];
      collectBindings(declaration.name, names);
      for (const name of names) {
        declarations.push({ name, filePath });
      }
    }
  }

  return declarations;
}

function assertValidBundleModules(moduleFiles, bundleName) {
  const declarationsByName = new Map();

  for (const moduleFile of moduleFiles) {
    resolveSourceModulePath(moduleFile);

    const filePath = path.join(distDir, moduleFile);
    if (!fs.existsSync(filePath)) {
      throw new Error(`Built module not found for ${bundleName}: ${moduleFile}`);
    }

    for (const declaration of topLevelRuntimeDeclarations(filePath)) {
      const matches = declarationsByName.get(declaration.name) || [];
      matches.push(declaration.filePath);
      declarationsByName.set(declaration.name, matches);
    }
  }

  const duplicates = Array.from(declarationsByName.entries())
    .filter(([, files]) => files.length > 1)
    .sort(([a], [b]) => a.localeCompare(b));

  if (duplicates.length > 0) {
    const detail = duplicates
      .map(([name, files]) => `${name}: ${files.map((file) => path.basename(file)).join(', ')}`)
      .join('\n');
    throw new Error(`Duplicate top-level runtime declarations in ${bundleName} bundle:\n${detail}`);
  }
}

function bundleModules(moduleFiles, bundleName) {
  assertValidBundleModules(moduleFiles, bundleName);

  let bundledContent = '';
  for (const moduleFile of moduleFiles) {
    const filePath = path.join(distDir, moduleFile);
    if (!fs.existsSync(filePath)) {
      throw new Error(`Module file not found for ${bundleName}: ${moduleFile}`);
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
const userscriptContent = header + bundleModules(userscriptModules, 'userscript') + footer;
fs.writeFileSync(userscriptPath, userscriptContent, 'utf8');
console.log('\n✓ Build complete: wayback-userscript.js');
console.log(`✓ Bundle size: ${userscriptContent.length} bytes`);

// Build Puppeteer bundle with pako embedded
const pakoContent = fs.readFileSync(pakoPath, 'utf8');
const puppeteerContent = `(function() {
'use strict';

// === pako.min.js ===
${pakoContent}

${bundleModules(puppeteerModules, 'puppeteer')}
})();
`;
fs.writeFileSync(puppeteerPath, puppeteerContent, 'utf8');
fs.writeFileSync(distPackageJSONPath, JSON.stringify({ type: 'module' }, null, 2) + '\n', 'utf8');
console.log('✓ Build complete: wayback-puppeteer.js');
console.log(`✓ Bundle size: ${puppeteerContent.length} bytes`);
console.log('✓ Wrote dist/package.json');
