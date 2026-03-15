// Test DOMCollector textKey dedup logic against real duplicate tweet patterns
// Run: node tests/browser/test-dedup.js
//
// Verifies that textKey correctly normalizes dynamic content
// (video timestamps, relative times) so dedup catches duplicates.

let passed = 0;
let failed = 0;

function assert(condition, msg) {
  if (condition) {
    passed++;
    console.log(`  ✓ ${msg}`);
  } else {
    failed++;
    console.error(`  ✗ ${msg}`);
  }
}

// Replicate OLD textKey logic
function textKeyOld(html) {
  const text = html.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();
  const imgs = [];
  const imgRe = /<img[^>]+src="([^"]+)"/g;
  let m;
  while ((m = imgRe.exec(html)) !== null) imgs.push(m[1]);
  return text + (imgs.length > 0 ? '\0' + imgs.join('\0') : '');
}

// Replicate NEW textKey logic (matches dom-collector.ts)
function textKeyNew(html) {
  let text = html.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();
  text = text.replace(/\d+:\d+(:\d+)?\s*\/\s*\d+:\d+(:\d+)?/g, '');
  text = text.replace(/(?<=·)\d+[smhd]/g, '');
  const imgs = [];
  const imgRe = /<img[^>]+src="([^"]+)"/g;
  let m;
  while ((m = imgRe.exec(html)) !== null) imgs.push(m[1]);
  return text + (imgs.length > 0 ? '\0' + imgs.join('\0') : '');
}

// --- Test 1: Video timestamp normalization ---
console.log('\nTest 1: Video timestamps produce same textKey');
{
  const tweet1 = '<article><div>阿刁和麦芽 0:03 / 2:14</div><img src="a.jpg"></article>';
  const tweet2 = '<article><div>阿刁和麦芽 0:00 / 2:14</div><img src="a.jpg"></article>';

  assert(textKeyOld(tweet1) !== textKeyOld(tweet2), 'Old textKey: timestamps differ (expected)');
  assert(textKeyNew(tweet1) === textKeyNew(tweet2), 'New textKey: timestamps normalized');
}

// --- Test 2: Relative time normalization (minutes) ---
console.log('\nTest 2: Relative times (minutes) produce same textKey');
{
  const tweet1 = '<article><div>user·40m some tweet text</div></article>';
  const tweet2 = '<article><div>user·41m some tweet text</div></article>';

  assert(textKeyOld(tweet1) !== textKeyOld(tweet2), 'Old textKey: times differ (expected)');
  assert(textKeyNew(tweet1) === textKeyNew(tweet2), 'New textKey: times normalized');
}

// --- Test 3: Relative time normalization (hours) ---
console.log('\nTest 3: Relative times (hours) produce same textKey');
{
  const tweet1 = '<article><div>user·14h tweet content here</div></article>';
  const tweet2 = '<article><div>user·15h tweet content here</div></article>';

  assert(textKeyNew(tweet1) === textKeyNew(tweet2), 'New textKey: hours normalized');
}

// --- Test 4: Don't strip legitimate numbers ---
console.log('\nTest 4: Legitimate numbers preserved');
{
  const tweet1 = '<article><div>ETH $2,100.18 price target</div></article>';
  const tweet2 = '<article><div>ETH $2,200.50 price target</div></article>';

  assert(textKeyNew(tweet1) !== textKeyNew(tweet2), 'Different prices produce different keys');
}

// --- Test 5: Long video timestamps (h:mm:ss) ---
console.log('\nTest 5: Long video timestamps (h:mm:ss)');
{
  const tweet1 = '<article><div>podcast 1:23:45 / 2:00:00</div></article>';
  const tweet2 = '<article><div>podcast 0:05:12 / 2:00:00</div></article>';

  assert(textKeyNew(tweet1) === textKeyNew(tweet2), 'Long timestamps normalized');
}

// --- Test 6: Relative time with seconds and days ---
console.log('\nTest 6: Seconds and days relative times');
{
  const t1 = '<article><div>user·2s just now</div></article>';
  const t2 = '<article><div>user·5s just now</div></article>';
  assert(textKeyNew(t1) === textKeyNew(t2), 'Seconds normalized');

  const t3 = '<article><div>user·3d old tweet</div></article>';
  const t4 = '<article><div>user·4d old tweet</div></article>';
  assert(textKeyNew(t3) === textKeyNew(t4), 'Days normalized');
}

// --- Test 7: Don't strip time-like patterns in normal text ---
console.log('\nTest 7: Preserve non-timestamp time patterns');
{
  const t1 = '<article><div>meeting at 10:30 tomorrow</div></article>';
  const t2 = '<article><div>meeting at 11:00 tomorrow</div></article>';
  // These don't have the "X / Y" pattern, so they should NOT be stripped
  assert(textKeyNew(t1) !== textKeyNew(t2), 'Non-timestamp times preserved');
}

// --- Test 8: Don't strip numbers that aren't relative times ---
console.log('\nTest 8: Preserve numbers without time-unit suffix');
{
  const t1 = '<article><div>user·100 followers</div></article>';
  const t2 = '<article><div>user·200 followers</div></article>';
  assert(textKeyNew(t1) !== textKeyNew(t2), 'Plain numbers preserved');
}

// --- Test 9: Real X.com tweet patterns from page 1659 ---
console.log('\nTest 9: Real X.com patterns');
{
  // 手表挖矿 - differs by "40m" vs "41m"
  const a = '<article><div>U.ETH @Bitwux·40m手表挖矿来了</div><img src="watch.jpg"></article>';
  const b = '<article><div>U.ETH @Bitwux·41m手表挖矿来了</div><img src="watch.jpg"></article>';
  assert(textKeyNew(a) === textKeyNew(b), 'Real: 40m vs 41m normalized');

  // 在我大学 - differs by "54m" vs "56m"
  const c = '<article><div>th@Super4DeFi·54m在我大学的时候</div></article>';
  const d = '<article><div>th@Super4DeFi·56m在我大学的时候</div></article>';
  assert(textKeyNew(c) === textKeyNew(d), 'Real: 54m vs 56m normalized');

  // 拉布拉多 - differs by "23m" vs "24m"
  const e = '<article><div>u@joyliumacro·23m拉布拉多 9 岁半了</div></article>';
  const f = '<article><div>u@joyliumacro·24m拉布拉多 9 岁半了</div></article>';
  assert(textKeyNew(e) === textKeyNew(f), 'Real: 23m vs 24m normalized');

  // Reddit CLI - differs by video timestamp
  const g = '<article><div>请码：NM-NTYR-P96Q0:03 / 3:32Quote</div></article>';
  const h = '<article><div>请码：NM-NTYR-P96QQuote</div></article>';
  assert(textKeyNew(g) === textKeyNew(h), 'Real: video timestamp present vs absent');
}

// --- Summary ---
console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
