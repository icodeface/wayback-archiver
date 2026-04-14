// 模拟 GM_xmlhttpRequest
function GM_xmlhttpRequest(options) {
    const https = require('https');
    const http = require('http');
    
    const url = new URL(options.url);
    const client = url.protocol === 'https:' ? https : http;
    
    const req = client.request({
        hostname: url.hostname,
        port: url.port || (url.protocol === 'https:' ? 443 : 80),
        path: url.pathname,
        method: options.method,
        headers: options.headers
    }, (res) => {
        let data = '';
        res.on('data', chunk => data += chunk);
        res.on('end', () => {
            if (options.onload) {
                options.onload({
                    status: res.statusCode,
                    statusText: res.statusMessage,
                    responseText: data
                });
            }
        });
    });
    
    req.on('error', (error) => {
        if (options.onerror) {
            options.onerror(error);
        }
    });
    
    if (options.data) {
        req.write(options.data);
    }
    req.end();
}

// 测试数据
const testData = {
    url: 'https://example.com',
    title: 'Test Page',
    timestamp: Date.now(),
    html: '<html><body>Test</body></html>'
};

console.log('Sending test archive request...');
GM_xmlhttpRequest({
    method: 'POST',
    url: 'http://localhost:8080/api/archive',
    headers: {
        'Content-Type': 'application/json'
    },
    data: JSON.stringify(testData),
    onload: (response) => {
        console.log('✓ Success:', response.status, response.statusText);
        console.log('Response:', response.responseText);
        process.exit(0);
    },
    onerror: (error) => {
        console.error('✗ Error:', error.message);
        process.exit(1);
    }
});
