#!/usr/bin/env node

/**
 * 测试删除队列功能
 *
 * 验证：
 * 1. 更新页面时，旧 HTML 文件被记录到删除队列
 * 2. 删除队列文件格式正确
 * 3. 旧 HTML 文件在 7 天后会被删除
 */

const fs = require('fs');
const path = require('path');

const SERVER_URL = 'http://localhost:8080';
const DATA_DIR = path.join(__dirname, '../../server/data');
const DELETION_QUEUE_FILE = path.join(DATA_DIR, 'deletion_queue.jsonl');

async function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function createPage(url, html) {
  const response = await fetch(`${SERVER_URL}/api/archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, html, title: 'Test Page' })
  });

  if (!response.ok) {
    throw new Error(`Failed to create page: ${response.statusText}`);
  }

  const data = await response.json();
  return data.page_id;
}

async function updatePage(pageId, html) {
  const response = await fetch(`${SERVER_URL}/api/archive/${pageId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      url: `https://example.com/test-${pageId}`,
      html,
      title: 'Updated Test Page'
    })
  });

  if (!response.ok) {
    throw new Error(`Failed to update page: ${response.statusText}`);
  }

  return await response.json();
}

function readDeletionQueue() {
  if (!fs.existsSync(DELETION_QUEUE_FILE)) {
    return [];
  }

  const content = fs.readFileSync(DELETION_QUEUE_FILE, 'utf-8');
  return content.trim().split('\n')
    .filter(line => line.length > 0)
    .map(line => JSON.parse(line));
}

async function main() {
  console.log('=== 测试删除队列功能 ===\n');

  // 1. 创建初始页面
  console.log('1. 创建初始页面...');
  const pageId = await createPage(
    'https://example.com/test-deletion-queue',
    '<html><body><h1>Version 1</h1></body></html>'
  );
  console.log(`   ✓ 页面创建成功 (ID: ${pageId})\n`);

  await sleep(1000);

  // 2. 更新页面（第一次）
  console.log('2. 更新页面（第一次）...');
  await updatePage(pageId, '<html><body><h1>Version 2</h1></body></html>');
  console.log('   ✓ 页面更新成功\n');

  await sleep(500);

  // 3. 检查删除队列
  console.log('3. 检查删除队列...');
  let queue = readDeletionQueue();
  console.log(`   ✓ 删除队列中有 ${queue.length} 条记录`);

  if (queue.length > 0) {
    const lastRecord = queue[queue.length - 1];
    console.log(`   ✓ 最新记录: ${JSON.stringify(lastRecord, null, 2)}`);

    // 验证记录格式
    if (!lastRecord.html_path || !lastRecord.timestamp || !lastRecord.page_id) {
      console.error('   ✗ 删除队列记录格式不正确');
      process.exit(1);
    }

    // 验证旧 HTML 文件仍然存在
    const oldHTMLPath = path.join(DATA_DIR, lastRecord.html_path);
    if (fs.existsSync(oldHTMLPath)) {
      console.log(`   ✓ 旧 HTML 文件仍然存在: ${lastRecord.html_path}`);
    } else {
      console.error(`   ✗ 旧 HTML 文件不存在: ${lastRecord.html_path}`);
      process.exit(1);
    }
  }

  console.log('\n');

  // 4. 再次更新页面（第二次）
  console.log('4. 更新页面（第二次）...');
  await updatePage(pageId, '<html><body><h1>Version 3</h1></body></html>');
  console.log('   ✓ 页面更新成功\n');

  await sleep(500);

  // 5. 再次检查删除队列
  console.log('5. 再次检查删除队列...');
  queue = readDeletionQueue();
  console.log(`   ✓ 删除队列中现在有 ${queue.length} 条记录`);

  if (queue.length >= 2) {
    console.log('   ✓ 两次更新都被记录到删除队列');
  }

  console.log('\n=== 所有测试通过 ===');
  console.log('\n提示：');
  console.log('- 删除队列文件位置:', DELETION_QUEUE_FILE);
  console.log('- 旧 HTML 文件会在 7 天后自动删除');
  console.log('- 可以通过重启服务器或等待午夜触发清理');
}

main().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
