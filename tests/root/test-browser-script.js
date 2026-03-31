const puppeteer = require('puppeteer');
const fs = require('fs');
const path = require('path');

// 读取编译后的脚本
const scriptPath = path.join(__dirname, 'browser/dist/wayback-userscript.js');
const userScript = fs.readFileSync(scriptPath, 'utf8');

// 提取脚本内容（去掉 UserScript 头部）
const scriptContent = userScript.split('// ==/UserScript==')[1];

async function testArchiveScript() {
    console.log('🚀 Starting Puppeteer test...\n');

    const browser = await puppeteer.launch({
        headless: false, // 显示浏览器窗口以便观察
        executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
        args: ['--no-sandbox', '--disable-setuid-sandbox']
    });

    const page = await browser.newPage();

    // 监听控制台输出
    page.on('console', msg => {
        const text = msg.text();
        if (text.includes('[Wayback]')) {
            console.log('📝', text);
        }
    });

    // 监听网络请求
    let archiveRequestSent = false;
    page.on('request', request => {
        if (request.url().includes('/api/archive')) {
            console.log('✅ Archive request detected:', request.method(), request.url());
            archiveRequestSent = true;
        }
    });

    page.on('response', response => {
        if (response.url().includes('/api/archive')) {
            console.log('✅ Archive response:', response.status(), response.statusText());
        }
    });

    try {
        console.log('📄 Test 1: Load page and wait for auto-send\n');

        // 访问测试页面
        await page.goto('https://example.com', { waitUntil: 'networkidle0' });

        // 注入脚本
        await page.evaluateOnNewDocument(scriptContent);
        await page.evaluate(scriptContent);

        console.log('⏳ Waiting 8 seconds for auto-send (2s prepare + 5s delay + 1s buffer)...');
        await page.waitForTimeout(8000);

        if (archiveRequestSent) {
            console.log('✅ Test 1 PASSED: Auto-send worked\n');
        } else {
            console.log('❌ Test 1 FAILED: No archive request sent\n');
        }

        // 重置标志
        archiveRequestSent = false;

        console.log('📄 Test 2: Close tab before auto-send\n');

        // 打开新页面
        const page2 = await browser.newPage();

        page2.on('console', msg => {
            const text = msg.text();
            if (text.includes('[Wayback]')) {
                console.log('📝', text);
            }
        });

        page2.on('request', request => {
            if (request.url().includes('/api/archive')) {
                console.log('✅ Archive request detected on close:', request.method());
                archiveRequestSent = true;
            }
        });

        await page2.goto('https://example.com', { waitUntil: 'networkidle0' });
        await page2.evaluateOnNewDocument(scriptContent);
        await page2.evaluate(scriptContent);

        console.log('⏳ Waiting 3 seconds (let data prepare)...');
        await page2.waitForTimeout(3000);

        console.log('🔴 Closing tab...');
        await page2.close();

        // 等待一下看请求是否发送
        await page.waitForTimeout(1000);

        if (archiveRequestSent) {
            console.log('✅ Test 2 PASSED: Close event triggered archive\n');
        } else {
            console.log('❌ Test 2 FAILED: Close did not trigger archive\n');
        }

        console.log('📄 Test 3: Navigate away\n');

        archiveRequestSent = false;
        const page3 = await browser.newPage();

        page3.on('request', request => {
            if (request.url().includes('/api/archive')) {
                console.log('✅ Archive request detected on navigate');
                archiveRequestSent = true;
            }
        });

        await page3.goto('https://example.com', { waitUntil: 'networkidle0' });
        await page3.evaluateOnNewDocument(scriptContent);
        await page3.evaluate(scriptContent);

        await page3.waitForTimeout(3000);

        console.log('🔄 Navigating to another page...');
        await page3.goto('https://example.org', { waitUntil: 'networkidle0' });

        await page.waitForTimeout(1000);

        if (archiveRequestSent) {
            console.log('✅ Test 3 PASSED: Navigation triggered archive\n');
        } else {
            console.log('❌ Test 3 FAILED: Navigation did not trigger archive\n');
        }

    } catch (error) {
        console.error('❌ Test error:', error);
    } finally {
        console.log('\n🏁 Tests completed. Closing browser in 3 seconds...');
        await page.waitForTimeout(3000);
        await browser.close();
    }
}

// 运行测试
testArchiveScript().catch(console.error);
