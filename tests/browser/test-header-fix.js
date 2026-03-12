const puppeteer = require('puppeteer');

async function testHeaderOverlap() {
  const browser = await puppeteer.launch({
    headless: true,
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });

  try {
    const page = await browser.newPage();
    await page.setViewport({ width: 1280, height: 800 });

    // 访问归档页面
    console.log('访问归档页面: http://localhost:8080/view/855');
    await page.goto('http://localhost:8080/view/855', {
      waitUntil: 'networkidle2',
      timeout: 30000
    });

    // 等待页面加载完成
    await new Promise(resolve => setTimeout(resolve, 2000));

    // 检查归档 header 是否存在
    const archiveHeader = await page.$('#wayback-archive-header');
    if (!archiveHeader) {
      console.error('❌ 归档 header 未找到');
      return false;
    }
    console.log('✓ 归档 header 存在');

    // 获取归档 header 的高度和位置
    const headerBox = await archiveHeader.boundingBox();
    console.log(`归档 header 位置: top=${headerBox.y}, height=${headerBox.height}`);

    // 查找所有 fixed/sticky 定位的元素
    const positionedElements = await page.evaluate(() => {
      const elements = [];
      const allElements = document.querySelectorAll('*:not(#wayback-archive-header):not(#wayback-archive-header *)');

      allElements.forEach(el => {
        const style = window.getComputedStyle(el);
        const position = style.position;
        const top = style.top;
        const zIndex = style.zIndex;

        if ((position === 'fixed' || position === 'sticky') &&
            (top === '0px' || parseInt(top) >= 0 && parseInt(top) <= 50)) {
          const rect = el.getBoundingClientRect();
          elements.push({
            tag: el.tagName,
            className: el.className,
            id: el.id,
            position: position,
            top: top,
            zIndex: zIndex,
            rect: {
              top: rect.top,
              left: rect.left,
              width: rect.width,
              height: rect.height
            }
          });
        }
      });

      return elements;
    });

    console.log(`\n找到 ${positionedElements.length} 个 fixed/sticky 定位的顶部元素:`);
    positionedElements.forEach((el, index) => {
      console.log(`\n元素 ${index + 1}:`);
      console.log(`  标签: ${el.tag}`);
      console.log(`  类名: ${el.className || '(无)'}`);
      console.log(`  ID: ${el.id || '(无)'}`);
      console.log(`  position: ${el.position}`);
      console.log(`  top: ${el.top}`);
      console.log(`  z-index: ${el.zIndex}`);
      console.log(`  实际位置: top=${el.rect.top}, left=${el.rect.left}`);
      console.log(`  尺寸: width=${el.rect.width}, height=${el.rect.height}`);

      // 检查是否与归档 header 重叠
      const headerBottom = headerBox.y + headerBox.height;
      const isVisible = el.rect.width > 0 && el.rect.height > 0;
      if (!isVisible) {
        console.log(`  ⏭️  跳过: 不可见元素 (尺寸为 0)`);
      } else if (el.rect.top < headerBottom) {
        console.log(`  ⚠️  警告: 此元素与归档 header 重叠! (元素 top=${el.rect.top} < header bottom=${headerBottom})`);
      } else {
        console.log(`  ✓ 此元素未与归档 header 重叠`);
      }
    });

    // 截图保存
    await page.screenshot({ path: 'tests/browser/header-test-screenshot.png', fullPage: true });
    console.log('\n✓ 截图已保存到 tests/browser/header-test-screenshot.png');

    // 检查是否有可见元素重叠（忽略零尺寸的不可见元素）
    const hasOverlap = positionedElements.some(el => {
      const headerBottom = headerBox.y + headerBox.height;
      const isVisible = el.rect.width > 0 && el.rect.height > 0;
      return isVisible && el.rect.top < headerBottom;
    });

    if (hasOverlap) {
      console.log('\n❌ 测试失败: 发现可见元素与归档 header 重叠');
      return false;
    } else {
      console.log('\n✅ 测试通过: 所有可见的 fixed/sticky 元素都已正确下移，未与归档 header 重叠');
      return true;
    }

  } catch (error) {
    console.error('测试出错:', error);
    return false;
  } finally {
    await browser.close();
  }
}

// 运行测试
testHeaderOverlap().then(success => {
  process.exit(success ? 0 : 1);
});
