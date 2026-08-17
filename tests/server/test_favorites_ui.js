// 收藏功能的浏览器端 e2e 测试。
// 验证首页星标按钮的状态回显与切换，以及 /favorites 页面的列表、搜索和取消收藏。
//
// 前置条件：服务运行在 BASE_URL（默认 http://localhost:8099），且库中已有归档页面。
const puppeteer = require('puppeteer');

const BASE_URL = process.env.BASE_URL || 'http://localhost:8080';
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

const failures = [];

function check(name, condition, detail) {
  if (condition) {
    console.log(`  ✓ ${name}`);
  } else {
    console.log(`  ✗ ${name}${detail ? ` — ${detail}` : ''}`);
    failures.push(name);
  }
}

async function api(path, options = {}) {
  const res = await fetch(`${BASE_URL}${path}`, options);
  const text = await res.text();
  let body = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = text;
  }
  return { status: res.status, body };
}

async function resetFavorites() {
  const { body } = await api('/api/favorites?limit=1000');
  for (const page of body.pages || []) {
    await api(`/api/favorites/${page.id}`, { method: 'DELETE' });
  }
}

async function openPage(browser) {
  const page = await browser.newPage();
  page.on('pageerror', (err) => {
    failures.push(`页面 JS 异常: ${err.message}`);
    console.log(`  ✗ 页面 JS 异常: ${err.message}`);
  });
  page.on('dialog', (dialog) => dialog.dismiss().catch(() => {}));
  return page;
}

// 首页：星标按钮切换收藏，并在刷新后保持状态
async function testIndexToggle(browser, pageId) {
  console.log('\n[1] 首页星标按钮');
  const page = await openPage(browser);
  try {
    await page.goto(`${BASE_URL}/`, { waitUntil: 'networkidle2' });

    const sel = `.favorite-btn[data-page-id="${pageId}"]`;
    await page.waitForSelector(sel, { timeout: 10000 });

    const initial = await page.$eval(sel, (el) => el.classList.contains('favorited'));
    check('初始未收藏', initial === false, `favorited=${initial}`);

    await page.click(sel);
    await page.waitForFunction(
      (s) => document.querySelector(s)?.classList.contains('favorited'),
      { timeout: 5000 },
      sel,
    );
    check('点击后按钮变为已收藏', true);

    const afterClick = await api(`/api/favorites/${pageId}/status`);
    check('服务端状态已写入', afterClick.body.is_favorite === true, JSON.stringify(afterClick.body));

    // 刷新后应从服务端回显已收藏状态
    await page.reload({ waitUntil: 'networkidle2' });
    await page.waitForSelector(sel, { timeout: 10000 });
    await page.waitForFunction(
      (s) => document.querySelector(s)?.classList.contains('favorited'),
      { timeout: 5000 },
      sel,
    ).catch(() => {});
    const afterReload = await page.$eval(sel, (el) => el.classList.contains('favorited'));
    check('刷新后状态保持', afterReload === true, `favorited=${afterReload}`);

    // 再次点击取消收藏
    await page.click(sel);
    await page.waitForFunction(
      (s) => !document.querySelector(s)?.classList.contains('favorited'),
      { timeout: 5000 },
      sel,
    );
    const removed = await api(`/api/favorites/${pageId}/status`);
    check('再次点击取消收藏', removed.body.is_favorite === false, JSON.stringify(removed.body));

    // 点击星标不应打开归档页面（事件冒泡被阻止）
    const pagesOpen = (await browser.pages()).length;
    await page.click(sel);
    await new Promise((r) => setTimeout(r, 800));
    check('点击星标不触发打开快照', (await browser.pages()).length === pagesOpen);
  } finally {
    await page.close();
  }
}

// /favorites 页面：列表渲染、搜索、取消收藏
async function testFavoritesPage(browser, favId, otherId) {
  console.log('\n[2] /favorites 页面');
  const page = await openPage(browser);
  try {
    await api(`/api/favorites/${favId}`, { method: 'POST' });
    await api(`/api/favorites/${otherId}`, { method: 'DELETE' });

    await page.goto(`${BASE_URL}/favorites`, { waitUntil: 'networkidle2' });
    await page.waitForSelector('.page-item', { timeout: 10000 });

    const ids = await page.$$eval('.page-id', (els) => els.map((e) => e.textContent.trim()));
    check('只列出已收藏页面', ids.length === 1 && ids[0] === `#${favId}`, JSON.stringify(ids));

    const hasUnfavBtn = await page.$('.unfavorite-btn');
    check('渲染取消收藏按钮', hasUnfavBtn !== null);

    // 标题使用 textContent 渲染，不应出现未转义标记
    const titleHTML = await page.$eval('.page-title', (el) => el.innerHTML);
    check('标题无注入标签', !/<script|<img/i.test(titleHTML), titleHTML.slice(0, 80));

    // 取消收藏后列表应变为空
    await page.click('.unfavorite-btn');
    await page.waitForFunction(
      () => document.querySelector('#pageList .empty') !== null
        || document.querySelectorAll('.page-item').length === 0,
      { timeout: 8000 },
    );
    const remaining = await api('/api/favorites');
    check('取消收藏后服务端为空', remaining.body.total === 0, JSON.stringify(remaining.body.total));
  } finally {
    await page.close();
  }
}

// /favorites 页面的搜索框
async function testFavoritesSearch(browser, favId, keyword, otherId) {
  console.log('\n[3] /favorites 搜索');
  const page = await openPage(browser);
  try {
    await api(`/api/favorites/${favId}`, { method: 'POST' });
    await api(`/api/favorites/${otherId}`, { method: 'POST' });

    await page.goto(`${BASE_URL}/favorites`, { waitUntil: 'networkidle2' });
    await page.waitForSelector('.page-item', { timeout: 10000 });
    const before = await page.$$eval('.page-item', (els) => els.length);
    check('搜索前列出两条', before === 2, `count=${before}`);

    const input = await page.$('#searchInput');
    check('存在搜索框', input !== null);
    if (input) {
      await input.type(keyword);
      await page.waitForFunction(
        () => document.querySelectorAll('.page-item').length === 1,
        { timeout: 8000 },
      );
      const ids = await page.$$eval('.page-id', (els) => els.map((e) => e.textContent.trim()));
      check('搜索命中唯一收藏', ids.length === 1 && ids[0] === `#${favId}`, JSON.stringify(ids));
    }
  } finally {
    await page.close();
  }
}

// 从候选页面里找一对，且存在只匹配 target 的关键字（避免依赖特定测试数据）
function pickSearchablePair(list) {
  const haystack = (p) => `${p.url}\n${p.title || ''}`.toLowerCase();
  for (let i = 0; i < list.length; i += 1) {
    for (let j = 0; j < list.length; j += 1) {
      if (i === j) continue;
      const target = list[i];
      const other = list[j];
      const otherText = haystack(other);
      const tokens = (target.url.match(/[A-Za-z0-9]{4,}/g) || [])
        .map((t) => t.toLowerCase())
        .filter((t) => !otherText.includes(t));
      if (tokens.length > 0) {
        return { target, other, keyword: tokens[tokens.length - 1] };
      }
    }
  }
  return null;
}

async function main() {
  const version = await api('/api/version');
  if (version.status !== 200) {
    console.error(`服务未就绪：${BASE_URL}（先启动 wayback-server）`);
    process.exit(1);
  }
  console.log(`服务: ${BASE_URL} version=${version.body.version}`);

  const pages = await api('/api/pages?limit=5');
  const list = pages.body.pages || pages.body;
  if (!Array.isArray(list) || list.length < 2) {
    console.error('需要至少 2 个已归档页面');
    process.exit(1);
  }
  // 挑一对页面，要求能找到只命中其中一个的搜索关键字
  const pair = pickSearchablePair(list);
  if (!pair) {
    console.error('找不到可区分的页面对（需要两个 URL 有不同的路径片段）');
    process.exit(1);
  }
  const { target, other, keyword } = pair;
  console.log(`测试页面: #${target.id} / #${other.id}，搜索关键字: ${keyword}`);

  // 本测试会增删收藏，不能在已有收藏数据的库上跑（会破坏用户数据）
  const existing = await api('/api/favorites?limit=1');
  if (existing.body.total > 0) {
    console.log(`\nSKIP: 当前库已有 ${existing.body.total} 条收藏，跳过以避免破坏数据。`);
    console.log('（用空库运行：DB_PATH=/tmp/x.db DATA_DIR=/tmp/x ./bin/wayback-server）');
    process.exit(0);
  }

  const browser = await puppeteer.launch({
    headless: 'new',
    executablePath: CHROME,
    args: ['--no-sandbox'],
  });

  try {
    await testIndexToggle(browser, target.id);
    await resetFavorites();
    await testFavoritesPage(browser, target.id, other.id);
    await resetFavorites();
    await testFavoritesSearch(browser, target.id, keyword, other.id);
  } finally {
    await browser.close();
    await resetFavorites();
  }

  console.log('');
  if (failures.length > 0) {
    console.error(`FAIL: ${failures.length} 项未通过`);
    failures.forEach((f) => console.error(`  - ${f}`));
    process.exit(1);
  }
  console.log('PASS: 收藏 UI e2e 全部通过');
}

main().catch((err) => {
  console.error('测试异常:', err);
  process.exit(1);
});
