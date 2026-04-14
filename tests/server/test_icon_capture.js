const fs = require('fs');
const path = require('path');

async function testIconCapture() {
  console.log('Testing icon resource capture...\n');

  // 读取测试HTML
  const htmlPath = path.join(__dirname, 'test_icons.html');
  const html = fs.readFileSync(htmlPath, 'utf-8');

  // 模拟浏览器扩展发送的数据
  const captureData = {
    url: 'https://example.com/',
    title: 'Test Page - Icon Resources',
    html: html,
    timestamp: Date.now()
  };

  try {
    // 发送到服务器
    const response = await fetch('http://localhost:8080/api/archive', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(captureData)
    });

    const result = await response.json();
    console.log('Capture result:', result);

    if (result.status === 'success') {
      console.log('\n✓ Page captured successfully!');

      // 获取最新的页面ID
	      const pagesResponse = await fetch('http://localhost:8080/api/pages');
	      const pagesPayload = await pagesResponse.json();
	      const pages = Array.isArray(pagesPayload) ? pagesPayload : (pagesPayload.pages || []);
	      const latestPage = pages[0];

      console.log(`\nLatest page ID: ${latestPage.id}`);
      console.log(`Page URL: ${latestPage.url}`);
      console.log(`\nYou can view it at: http://localhost:8080/view/${latestPage.id}`);

      return latestPage.id;
    } else {
      console.error('\n✗ Capture failed');
      return null;
    }
  } catch (error) {
    console.error('Error:', error.message);
    return null;
  }
}

testIconCapture();
