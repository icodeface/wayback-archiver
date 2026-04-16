const puppeteer = require('puppeteer');
const { execSync } = require('child_process');
const path = require('path');

/**
 * 测试下载失败兜底逻辑：
 * 1. 直接往 DB + 磁盘写入一个资源（模拟之前成功下载过）
 * 2. 归档一个页面，引用同 URL（不可达，触发兜底）
 * 3. 验证页面渲染正常
 */

const WAYBACK = process.env.WAYBACK || 'http://localhost:8080';
const FAKE_URL = 'https://fallback-test-unreachable.invalid/fallback-style.css';
const CSS_CONTENT = 'body { background-color: #ff6600 !important; color: white; }';
const ROOT_DIR = path.resolve(__dirname, '..', '..');
const DATA_DIR = process.env.WAYBACK_DATA_DIR || path.join(ROOT_DIR, 'data');
const DB_USER = process.env.DB_USER || process.env.USER || 'postgres';

let passed = 0, failed = 0;
const failures = [];

function assert(name, cond, msg) {
  if (cond) { passed++; console.log(`  [PASS] ${name}`); }
  else { failed++; failures.push(`${name}: ${msg}`); console.log(`  [FAIL] ${name}: ${msg}`); }
}

async function test() {
  console.log('=== Download Fallback Test ===\n');

  const crypto = require('crypto');
  const fs = require('fs');

  // 计算 CSS 内容哈希
  const hash = crypto.createHash('sha256').update(CSS_CONTENT).digest('hex');

  // 写入资源文件到磁盘
  const resDir = path.join(DATA_DIR, 'resources', hash.slice(0, 2), hash.slice(2, 4));
  fs.mkdirSync(resDir, { recursive: true });
  const filePath = path.join(resDir, hash + '.css');
  fs.writeFileSync(filePath, CSS_CONTENT);
  const relPath = path.relative(DATA_DIR, filePath);
  console.log(`Step 1: Wrote resource file to disk: ${relPath}`);

  // 直接插入 DB 记录
  const insertSQL = `INSERT INTO resources (url, content_hash, resource_type, file_path, file_size, first_seen, last_seen)
    VALUES ('${FAKE_URL}', '${hash}', 'css', '${relPath}', ${CSS_CONTENT.length}, NOW(), NOW())
    ON CONFLICT DO NOTHING`;
  execSync(`psql -U ${DB_USER} -d wayback -c "${insertSQL}"`);
  console.log('  Inserted resource record into DB');

  // Step 2: 归档页面，引用该不可达 URL
  console.log('\nStep 2: Archive page referencing unreachable resource...');
  const uniqueTag = Date.now();
  const html = `<!DOCTYPE html>
<html><head><meta charset="utf-8">
<title>Fallback Verify ${uniqueTag}</title>
<link rel="stylesheet" href="${FAKE_URL}">
</head><body>
<h1>Fallback Test</h1>
<p>Server should fallback to DB resource.</p>
</body></html>`;

  const res = await fetch(`${WAYBACK}/api/archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      url: `https://fallback-verify-${uniqueTag}.example.com/page`,
      title: `Fallback Verify ${uniqueTag}`,
      html: html,
      timestamp: Date.now()
    })
  });
  const result = await res.json();
  console.log('  Result:', JSON.stringify(result));
  assert('Archive succeeds', result.status === 'success', JSON.stringify(result));

  // Step 3: Puppeteer 验证
  console.log('\nStep 3: Verify page renders with fallback CSS...');
  const searchRes = await fetch(`${WAYBACK}/api/search?q=${encodeURIComponent(`Fallback Verify ${uniqueTag}`)}`);
  const searchData = await searchRes.json();
  const pages = searchData.pages || searchData;
  const pageInfo = pages.find(p => p.title && p.title.includes(`${uniqueTag}`));

  if (!pageInfo) {
    failed++;
    failures.push('Could not find page in search');
    console.log('  [FAIL] Could not find page. Results:', JSON.stringify(searchData));
  } else {
    const rewrittenDeadline = Date.now() + 15000;
    let rewrittenHTML = '';
    while (Date.now() < rewrittenDeadline) {
      const viewRes = await fetch(`${WAYBACK}/view/${pageInfo.id}`);
      rewrittenHTML = await viewRes.text();
      if (rewrittenHTML.includes('/archive/') && rewrittenHTML.includes(FAKE_URL)) {
        break;
      }
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    assert('Archived HTML rewritten before render', rewrittenHTML.includes('/archive/'),
      'timed out waiting for async finalize to rewrite resource URLs');

    const browser = await puppeteer.launch({
      headless: true,
      executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
      args: ['--no-sandbox', '--disable-setuid-sandbox']
    });
    try {
      const page = await browser.newPage();
      const failedReqs = [];
      page.on('requestfailed', req => failedReqs.push(req.url()));

      const viewURL = `${WAYBACK}/view/${pageInfo.id}`;
      console.log(`  Loading: ${viewURL}`);
      const resp = await page.goto(viewURL, { waitUntil: 'networkidle0', timeout: 15000 });

      assert('Page loads (200)', resp.status() === 200, `Status: ${resp.status()}`);

      // 检查 CSS href 是否被重写为本地路径
      const cssHref = await page.evaluate(() => {
        const link = document.querySelector('link[rel="stylesheet"]');
        return link ? link.getAttribute('href') : null;
      });
      console.log(`  CSS href: ${cssHref}`);
      assert('CSS URL rewritten to local', cssHref && cssHref.startsWith('/archive/'),
        `href not rewritten to local proxy: ${cssHref}`);

      // 检查背景色
      const bgColor = await page.evaluate(() =>
        window.getComputedStyle(document.body).backgroundColor
      );
      console.log(`  Body background-color: ${bgColor}`);
      assert('CSS fallback applied (orange bg)', bgColor === 'rgb(255, 102, 0)',
        `Expected rgb(255, 102, 0), got: ${bgColor}`);

      const localFailures = failedReqs.filter(u => u.includes('localhost'));
      assert('No local resource failures', localFailures.length === 0,
        `Failed: ${localFailures.join(', ')}`);
    } finally {
      await browser.close();
    }
  }

  // Summary
  console.log('\n========================================');
  console.log('     DOWNLOAD FALLBACK TEST RESULTS');
  console.log('========================================');
  console.log(`Passed: ${passed}  Failed: ${failed}`);
  if (failures.length > 0) {
    console.log('\nFailures:');
    failures.forEach((f, i) => console.log(`  ${i + 1}. ${f}`));
  }
  console.log('========================================');
  process.exit(failed > 0 ? 1 : 0);
}

test().catch(err => { console.error('Test crashed:', err); process.exit(1); });
