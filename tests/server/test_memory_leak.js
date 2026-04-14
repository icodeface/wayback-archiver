/**
 * Memory Leak Detection Test
 *
 * 压力测试：大量归档请求 → 检查内存增长 → 触发 GC → 验证内存释放
 *
 * 测试场景：
 * 1. 基线内存采集
 * 2. 批量归档大 HTML 页面（模拟真实浏览场景）
 * 3. 批量更新页面（触发 UpdateCapture 路径）
 * 4. 批量查看归档页面（触发 ViewPage + ProxyResource 路径）
 * 5. 强制 GC → 验证内存回到合理范围
 * 6. Goroutine 数量不应持续增长
 */
const puppeteer = require('puppeteer');

const SERVER = 'http://localhost:8080';

// 生成指定大小的 HTML 内容（含内联样式，模拟 style-inliner 输出）
function generateLargeHTML(sizeKB, id) {
  const styles = Array.from({ length: 50 }, (_, i) =>
    `.item-${i} { display: flex; padding: ${i}px; margin: ${i % 10}px; background: #${(i * 111111 % 0xFFFFFF).toString(16).padStart(6, '0')}; font-size: ${12 + i % 8}px; line-height: 1.5; border-radius: ${i % 20}px; box-shadow: 0 ${i % 4}px ${i % 8}px rgba(0,0,0,0.${i % 5}); }`
  ).join('\n');

  const items = [];
  const baseContent = `<div class="item-0"><span>Content block for page ${id}</span></div>`;
  while (items.join('').length < sizeKB * 1024 - styles.length - 200) {
    items.push(`<div class="item-${items.length % 50}" data-idx="${items.length}"><p>Paragraph ${items.length} with some filler text for page ${id}. This ensures the HTML is large enough to test memory handling. Random: ${Math.random().toString(36).slice(2)}</p></div>`);
  }

  return `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Memory Test Page ${id}</title>
<style>${styles}</style></head>
<body>${items.join('\n')}</body></html>`;
}

async function getMemStats() {
  const res = await fetch(`${SERVER}/api/debug/memstats`);
  if (!res.ok) {
    throw new Error(`memstats_unavailable:${res.status}`);
  }
  return res.json();
}

async function forceGC() {
  const res = await fetch(`${SERVER}/api/debug/gc`, { method: 'POST' });
  if (!res.ok) {
    throw new Error(`gc_unavailable:${res.status}`);
  }
  return res.json();
}

async function archivePage(url, title, html) {
  const res = await fetch(`${SERVER}/api/archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, title, html }),
  });
  return res.json();
}

async function updatePage(pageId, url, title, html) {
  const res = await fetch(`${SERVER}/api/archive/${pageId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, title, html }),
  });
  return res.json();
}

async function deletePage(pageId) {
  await fetch(`${SERVER}/api/pages/${pageId}`, { method: 'DELETE' });
}

(async () => {
  console.log('=== Memory Leak Detection Test ===\n');

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
    condition ? pass(name) : fail(name, msg);
  }

  // ============================================================
  // Phase 0: Verify server is running
  // ============================================================
  try {
    await getMemStats();
  } catch (e) {
    try {
      const versionRes = await fetch(`${SERVER}/api/version`);
      if (versionRes.ok && String(e.message || '').startsWith('memstats_unavailable:')) {
        console.log('SKIP: debug API is disabled; set DEBUG_API=true to run memory leak test');
        process.exit(0);
      }
    } catch {
      // fall through to hard failure below
    }
    console.error('Server not running on localhost:8080. Start with: ./bin/wayback-server');
    process.exit(1);
  }

  // ============================================================
  // Phase 1: Baseline memory
  // ============================================================
  console.log('\n--- Phase 1: Baseline Memory ---');
  const gcBaseline = await forceGC();
  const baseline = await getMemStats();
  console.log(`  Heap: ${baseline.heap_alloc_mb.toFixed(2)} MB, Goroutines: ${baseline.goroutines}, GC cycles: ${baseline.gc_cycles}`);
  const baselineHeap = baseline.heap_alloc_mb;
  const baselineGoroutines = baseline.goroutines;

  // ============================================================
  // Phase 2: Batch archive large pages
  // ============================================================
  console.log('\n--- Phase 2: Archive 20 large pages (500KB each) ---');
  const pageIds = [];
  const PAGE_COUNT = 20;
  const PAGE_SIZE_KB = 500;
  const timestamp = Date.now();

  for (let i = 0; i < PAGE_COUNT; i++) {
    const url = `https://memtest-${timestamp}.example.com/page-${i}`;
    const html = generateLargeHTML(PAGE_SIZE_KB, i);
    const data = await archivePage(url, `MemTest Page ${i}`, html);
    if (data.status === 'success') {
      pageIds.push({ id: data.page_id, url });
    } else {
      console.log(`  Warning: archive failed for page ${i}: ${JSON.stringify(data)}`);
    }
    if ((i + 1) % 5 === 0) {
      const mem = await getMemStats();
      console.log(`  Archived ${i + 1}/${PAGE_COUNT}, Heap: ${mem.heap_alloc_mb.toFixed(2)} MB`);
    }
  }

  check('All pages archived', pageIds.length === PAGE_COUNT, `only ${pageIds.length}/${PAGE_COUNT} pages created`);

  const afterArchive = await getMemStats();
  console.log(`  After archive: Heap ${afterArchive.heap_alloc_mb.toFixed(2)} MB, Goroutines: ${afterArchive.goroutines}`);

  // ============================================================
  // Phase 3: GC after archive — verify memory can be reclaimed
  // ============================================================
  console.log('\n--- Phase 3: GC after archive ---');
  const afterArchiveGC = await forceGC();
  // Wait a moment for GC to complete fully
  await new Promise(r => setTimeout(r, 1000));
  const afterArchiveGCStats = await getMemStats();
  console.log(`  After GC: Heap ${afterArchiveGCStats.heap_alloc_mb.toFixed(2)} MB (was ${afterArchive.heap_alloc_mb.toFixed(2)} MB)`);

  const archiveReclaimed = afterArchive.heap_alloc_mb - afterArchiveGCStats.heap_alloc_mb;
  console.log(`  Reclaimed: ${archiveReclaimed.toFixed(2)} MB`);

  // ============================================================
  // Phase 4: Batch update pages (new content, triggers URL rewriting)
  // ============================================================
  console.log('\n--- Phase 4: Update all pages with new content ---');
  for (let i = 0; i < pageIds.length; i++) {
    const newHtml = generateLargeHTML(PAGE_SIZE_KB, `${i}-updated`);
    const data = await updatePage(pageIds[i].id, pageIds[i].url, `MemTest Updated ${i}`, newHtml);
    if (data.action !== 'updated') {
      console.log(`  Warning: update action for page ${i}: ${data.action}`);
    }
    if ((i + 1) % 5 === 0) {
      const mem = await getMemStats();
      console.log(`  Updated ${i + 1}/${pageIds.length}, Heap: ${mem.heap_alloc_mb.toFixed(2)} MB`);
    }
  }

  const afterUpdate = await getMemStats();
  console.log(`  After update: Heap ${afterUpdate.heap_alloc_mb.toFixed(2)} MB`);

  // ============================================================
  // Phase 5: GC after update
  // ============================================================
  console.log('\n--- Phase 5: GC after update ---');
  await forceGC();
  await new Promise(r => setTimeout(r, 1000));
  const afterUpdateGC = await getMemStats();
  console.log(`  After GC: Heap ${afterUpdateGC.heap_alloc_mb.toFixed(2)} MB (was ${afterUpdate.heap_alloc_mb.toFixed(2)} MB)`);

  const updateReclaimed = afterUpdate.heap_alloc_mb - afterUpdateGC.heap_alloc_mb;
  console.log(`  Reclaimed: ${updateReclaimed.toFixed(2)} MB`);

  // ============================================================
  // Phase 6: View all pages via Puppeteer (triggers ViewPage + ProxyResource)
  // ============================================================
  console.log('\n--- Phase 6: View all pages via Puppeteer ---');
  const browser = await puppeteer.launch({
    headless: true,
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  const memBeforeView = await getMemStats();
  console.log(`  Before viewing: Heap ${memBeforeView.heap_alloc_mb.toFixed(2)} MB`);

  for (let i = 0; i < pageIds.length; i++) {
    const page = await browser.newPage();
    try {
      await page.goto(`${SERVER}/view/${pageIds[i].id}`, {
        waitUntil: 'networkidle2',
        timeout: 15000,
      });
      // Verify page renders
      const title = await page.title();
      if (i === 0) {
        check('Archived page renders', title.includes('Memory Test Page') || title.includes('MemTest'), `title: ${title}`);
      }
    } catch (e) {
      console.log(`  Warning: failed to view page ${pageIds[i].id}: ${e.message}`);
    }
    await page.close();

    if ((i + 1) % 10 === 0) {
      const mem = await getMemStats();
      console.log(`  Viewed ${i + 1}/${pageIds.length}, Heap: ${mem.heap_alloc_mb.toFixed(2)} MB`);
    }
  }

  await browser.close();

  const afterView = await getMemStats();
  console.log(`  After viewing: Heap ${afterView.heap_alloc_mb.toFixed(2)} MB`);

  // ============================================================
  // Phase 7: Final GC — verify memory returns to reasonable level
  // ============================================================
  console.log('\n--- Phase 7: Final GC + verification ---');
  await forceGC();
  await new Promise(r => setTimeout(r, 2000));
  const finalStats = await getMemStats();
  console.log(`  Final:    Heap ${finalStats.heap_alloc_mb.toFixed(2)} MB, Goroutines: ${finalStats.goroutines}`);
  console.log(`  Baseline: Heap ${baselineHeap.toFixed(2)} MB, Goroutines: ${baselineGoroutines}`);
  console.log(`  Peak archive: ${afterArchive.heap_alloc_mb.toFixed(2)} MB`);
  console.log(`  Peak update:  ${afterUpdate.heap_alloc_mb.toFixed(2)} MB`);

  const memGrowth = finalStats.heap_alloc_mb - baselineHeap;
  const goroutineGrowth = finalStats.goroutines - baselineGoroutines;

  console.log(`\n  Memory growth from baseline: ${memGrowth.toFixed(2)} MB`);
  console.log(`  Goroutine growth: ${goroutineGrowth}`);

  // Assertions
  // Final heap should be within 30MB of baseline (cache may hold some data)
  check(
    'Memory reclaimed after GC (within 30MB of baseline)',
    memGrowth < 30,
    `heap grew ${memGrowth.toFixed(2)} MB from baseline — possible leak`
  );

  // Goroutines should not keep growing
  check(
    'No goroutine leak (growth < 5)',
    goroutineGrowth < 5,
    `goroutines grew by ${goroutineGrowth} — possible goroutine leak`
  );

  // GC should reclaim significant memory after archive peak
  const peakMem = Math.max(afterArchive.heap_alloc_mb, afterUpdate.heap_alloc_mb);
  const reclaimedRatio = (peakMem - finalStats.heap_alloc_mb) / peakMem;
  check(
    'GC reclaims >50% of peak memory',
    reclaimedRatio > 0.5 || peakMem < 20,
    `peak=${peakMem.toFixed(2)} MB, final=${finalStats.heap_alloc_mb.toFixed(2)} MB, reclaimed=${(reclaimedRatio * 100).toFixed(1)}%`
  );

  // ============================================================
  // Cleanup
  // ============================================================
  console.log('\n--- Cleanup ---');
  for (const p of pageIds) {
    await deletePage(p.id);
  }
  console.log(`  Deleted ${pageIds.length} test pages`);

  // ============================================================
  // Summary
  // ============================================================
  console.log('\n\n========================================');
  console.log('       MEMORY TEST RESULTS');
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

  console.log('\n--- Memory Timeline ---');
  console.log(`  Baseline:       ${baselineHeap.toFixed(2)} MB`);
  console.log(`  After archive:  ${afterArchive.heap_alloc_mb.toFixed(2)} MB`);
  console.log(`  After arch GC:  ${afterArchiveGCStats.heap_alloc_mb.toFixed(2)} MB`);
  console.log(`  After update:   ${afterUpdate.heap_alloc_mb.toFixed(2)} MB`);
  console.log(`  After upd GC:   ${afterUpdateGC.heap_alloc_mb.toFixed(2)} MB`);
  console.log(`  After view:     ${afterView.heap_alloc_mb.toFixed(2)} MB`);
  console.log(`  Final (GC):     ${finalStats.heap_alloc_mb.toFixed(2)} MB`);

  console.log('\n========================================');
  process.exit(failedTests > 0 ? 1 : 0);
})();
