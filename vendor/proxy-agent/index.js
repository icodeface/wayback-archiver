'use strict';

const http = require('node:http');
const https = require('node:https');
const { URL } = require('node:url');
const { HttpProxyAgent } = require('http-proxy-agent');
const { HttpsProxyAgent } = require('https-proxy-agent');
const { getProxyForUrl } = require('proxy-from-env');
const { SocksProxyAgent } = require('socks-proxy-agent');

// Puppeteer only needs env-driven proxy support here; omitting PAC keeps
// the dependency tree free of get-uri/basic-ftp.

function normalizeProtocol(protocol) {
  return protocol === 'https:' ? 'https:' : 'http:';
}

function buildRequestUrl(options) {
  const protocol = normalizeProtocol(options.protocol);
  const hostname = options.hostname || options.host || 'localhost';
  const auth = options.auth ? `${options.auth}@` : '';
  const port = options.port ? `:${options.port}` : '';
  const path = options.path || '/';

  return `${protocol}//${auth}${hostname}${port}${path}`;
}

function createProxyAgent(targetProtocol, proxyUrl) {
  const proxyProtocol = new URL(proxyUrl).protocol;
  if (proxyProtocol.startsWith('socks')) {
    return new SocksProxyAgent(proxyUrl);
  }

  if (targetProtocol === 'https:') {
    return new HttpsProxyAgent(proxyUrl);
  }

  return new HttpProxyAgent(proxyUrl);
}

class ProxyAgent extends http.Agent {
  constructor(options = {}) {
    super(options);
    this.options = options;
    this.agentCache = new Map();
  }

  addRequest(request, options) {
    const protocol = normalizeProtocol(options.protocol);
    const requestUrl = buildRequestUrl(options);
    const proxyUrl = getProxyForUrl(requestUrl);
    const agent = this.getAgent(protocol, proxyUrl);

    return agent.addRequest(request, options);
  }

  getAgent(protocol, proxyUrl) {
    if (!proxyUrl) {
      return protocol === 'https:' ? https.globalAgent : http.globalAgent;
    }

    const cacheKey = `${protocol}:${proxyUrl}`;
    let agent = this.agentCache.get(cacheKey);
    if (!agent) {
      agent = createProxyAgent(protocol, proxyUrl);
      this.agentCache.set(cacheKey, agent);
    }

    return agent;
  }

  destroy() {
    for (const agent of this.agentCache.values()) {
      if (typeof agent.destroy === 'function') {
        agent.destroy();
      }
    }
    this.agentCache.clear();
    super.destroy();
  }
}

module.exports = {
  ProxyAgent,
};
