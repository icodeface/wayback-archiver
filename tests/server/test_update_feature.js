const puppeteer = require('puppeteer');

const SERVER = 'http://localhost:8080';

(async () => {
  console.log('=== Wayback Page Update Feature Test ===\n');

  let totalTests = 0;
  let passedTests = 0;
  let failedTests = 0;
  const failures = [];

  function pass(name) {
    totalTests++;
    passedTests++;
    console.log(`  [PASS] ${name}`);
  }

  function fail(name, msg) {
    totalTests++;
    failedTests++;
    failures.push({ test: name, msg });
    console.log(`  [FAIL] ${name}: ${msg}`);
  }

  function check(name, condition, msg) {
    if (condition) {
      pass(name);
    } else {
      fail(name, msg);
    }
  }

  async function waitForPageTitle(pageId, expectedTitle, timeoutMs = 5000) {
    const startedAt = Date.now();
    while (Date.now() - startedAt < timeoutMs) {
      const res = await fetch(`${SERVER}/api/pages/${pageId}`);
      if (res.ok) {
        const page = await res.json();
        if (page && page.title === expectedTitle) {
          return;
        }
      }
      await new Promise((resolve) => setTimeout(resolve, 50));
    }

    throw new Error(`Timed out waiting for page ${pageId} title to become ${expectedTitle}`);
  }

  // ============================================================
  // Test 1: POST /api/archive returns page_id and action
  // ============================================================
  console.log('\n--- Test 1: Archive returns page_id and action ---');

  const originalHTML = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Update Test - Original</title>
<style>body { font-family: sans-serif; background: #eef; padding: 20px; }
.box { background: white; padding: 20px; border-radius: 8px; max-width: 600px; margin: 0 auto; }
h1 { color: #336; }</style></head>
<body><div class="box">
<h1>Original Content</h1>
<p>This is the original page content before any updates.</p>
<p>Item count: 3</p>
<ul><li>Item A</li><li>Item B</li><li>Item C</li></ul>
</div></body></html>`;

  const testURL = `https://test-update-feature.example.com/page-${Date.now()}`;

  const createRes = await fetch(`${SERVER}/api/archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: testURL, title: 'Update Test - Original', html: originalHTML })
  });
  const createData = await createRes.json();

  check('POST returns status success', createData.status === 'success', `got status: ${createData.status}`);
  check('POST returns page_id', typeof createData.page_id === 'number' && createData.page_id > 0, `got page_id: ${createData.page_id}`);
  check('POST returns action=created', createData.action === 'created', `got action: ${createData.action}`);

  const pageId = createData.page_id;
  console.log(`  Created page ID: ${pageId}`);

  // ============================================================
  // Test 2: POST same content returns unchanged
  // ============================================================
  console.log('\n--- Test 2: Duplicate archive returns unchanged ---');

  const dupRes = await fetch(`${SERVER}/api/archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: testURL, title: 'Update Test - Original', html: originalHTML })
  });
  const dupData = await dupRes.json();

  check('Duplicate returns unchanged', dupData.action === 'unchanged', `got action: ${dupData.action}`);
  check('Duplicate returns same page_id', dupData.page_id === pageId, `got page_id: ${dupData.page_id}, expected: ${pageId}`);

  // ============================================================
  // Test 3: Verify original page renders correctly
  // ============================================================
  console.log('\n--- Test 3: Original page renders correctly ---');

  const browser = await puppeteer.launch({
    headless: true,
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });

  let page = await browser.newPage();
  await page.goto(`${SERVER}/view/${pageId}`, { waitUntil: 'networkidle2', timeout: 15000 });

  const originalTitle = await page.title();
  check('Original page has correct title', originalTitle.includes('Update Test - Original'), `got title: ${originalTitle}`);

  const originalBodyText = await page.evaluate(() => document.body.innerText);
  check('Original page has original content', originalBodyText.includes('Original Content'), 'body does not contain "Original Content"');
  check('Original page has 3 items', originalBodyText.includes('Item A') && originalBodyText.includes('Item B') && originalBodyText.includes('Item C'), 'missing list items');

  const originalBg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  check('Original page has styled background', originalBg !== '' && originalBg !== 'rgba(0, 0, 0, 0)', `background: ${originalBg}`);

  await page.close();

  // ============================================================
  // Test 4: PUT /api/archive/:id updates content
  // ============================================================
  console.log('\n--- Test 4: PUT updates page content ---');

  const updatedHTML = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Update Test - Updated</title>
<style>body { font-family: sans-serif; background: #efe; padding: 20px; }
.box { background: white; padding: 20px; border-radius: 8px; max-width: 600px; margin: 0 auto; }
h1 { color: #363; }
.new-section { border-top: 2px solid #6a6; margin-top: 16px; padding-top: 16px; }</style></head>
<body><div class="box">
<h1>Updated Content</h1>
<p>This page has been updated with new content after DOM changes.</p>
<p>Item count: 5</p>
<ul><li>Item A</li><li>Item B</li><li>Item C</li><li>Item D (new)</li><li>Item E (new)</li></ul>
<div class="new-section">
<h2>Dynamically Loaded Section</h2>
<p>This section was loaded after the initial page capture.</p>
</div>
</div></body></html>`;

  const updateRes = await fetch(`${SERVER}/api/archive/${pageId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: testURL, title: 'Update Test - Updated', html: updatedHTML })
  });
  const updateData = await updateRes.json();

  check('PUT returns status success', updateData.status === 'success', `got status: ${updateData.status}`);
  check('PUT returns same page_id', updateData.page_id === pageId, `got page_id: ${updateData.page_id}, expected: ${pageId}`);
  check('PUT returns action=updated', updateData.action === 'updated', `got action: ${updateData.action}`);

  await waitForPageTitle(pageId, 'Update Test - Updated');

  // ============================================================
  // Test 5: PUT same content returns unchanged
  // ============================================================
  console.log('\n--- Test 5: PUT same content returns unchanged ---');

  const sameUpdateRes = await fetch(`${SERVER}/api/archive/${pageId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: testURL, title: 'Update Test - Updated', html: updatedHTML })
  });
  const sameUpdateData = await sameUpdateRes.json();

  check('PUT same content returns unchanged', sameUpdateData.action === 'unchanged', `got action: ${sameUpdateData.action}`);

  // ============================================================
  // Test 6: Verify updated page renders with new content
  // ============================================================
  console.log('\n--- Test 6: Updated page renders correctly ---');

  page = await browser.newPage();
  await page.goto(`${SERVER}/view/${pageId}`, { waitUntil: 'networkidle2', timeout: 15000 });

  const updatedTitle = await page.title();
  check('Updated page has new title', updatedTitle.includes('Update Test - Updated'), `got title: ${updatedTitle}`);

  const updatedBodyText = await page.evaluate(() => document.body.innerText);
  check('Updated page has new content', updatedBodyText.includes('Updated Content'), 'body does not contain "Updated Content"');
  check('Updated page has new items', updatedBodyText.includes('Item D (new)') && updatedBodyText.includes('Item E (new)'), 'missing new list items');
  check('Updated page has dynamic section', updatedBodyText.includes('Dynamically Loaded Section'), 'missing dynamic section');
  check('Original content is gone', !updatedBodyText.includes('Original Content'), 'old content still present');

  const updatedBg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  check('Updated page has new background color', updatedBg !== originalBg, `background unchanged: ${updatedBg}`);

  await page.close();

  // ============================================================
  // Test 7: Database has only one record for this URL
  // ============================================================
  console.log('\n--- Test 7: No duplicate pages created ---');

  const searchRes = await fetch(`${SERVER}/api/search?q=${encodeURIComponent('test-update-feature.example.com')}`);
  const searchData = await searchRes.json();
  const allPages = Array.isArray(searchData) ? searchData : (searchData.pages || []);
  const matchingPages = allPages.filter(p => p.url === testURL);

  check('Only one page record exists', matchingPages.length === 1, `found ${matchingPages.length} pages for URL`);

  if (matchingPages.length === 1) {
    check('Page record has updated title', matchingPages[0].title === 'Update Test - Updated', `title: ${matchingPages[0].title}`);
  }

  // ============================================================
  // Test 8: PUT to non-existent page returns error
  // ============================================================
  console.log('\n--- Test 8: Error handling ---');

  const badRes = await fetch(`${SERVER}/api/archive/999999999`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: 'http://x.com', title: 'X', html: '<html></html>' })
  });
  check('PUT non-existent page returns 500', badRes.status === 500, `got status: ${badRes.status}`);

  const badIdRes = await fetch(`${SERVER}/api/archive/notanumber`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: 'http://x.com', title: 'X', html: '<html></html>' })
  });
  check('PUT invalid ID returns 400', badIdRes.status === 400, `got status: ${badIdRes.status}`);

  // ============================================================
  // Cleanup: delete test page
  // ============================================================
  await fetch(`${SERVER}/api/pages/${pageId}`, { method: 'DELETE' });

  await browser.close();

  // ============================================================
  // Summary
  // ============================================================
  console.log('\n\n========================================');
  console.log('         TEST RESULTS SUMMARY');
  console.log('========================================');
  console.log(`Total:  ${totalTests}`);
  console.log(`Passed: ${passedTests}`);
  console.log(`Failed: ${failedTests}`);
  console.log(`Pass rate: ${((passedTests / totalTests) * 100).toFixed(1)}%`);

  if (failures.length > 0) {
    console.log('\n--- Failures ---');
    failures.forEach((f, i) => {
      console.log(`${i + 1}. ${f.test}: ${f.msg}`);
    });
  }

  console.log('\n========================================');
  process.exit(failedTests > 0 ? 1 : 0);
})();
