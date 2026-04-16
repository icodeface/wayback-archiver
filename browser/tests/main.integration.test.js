const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {
  createFakeBrowserEnvironment,
  flushMicrotasks,
  installGlobalBindings,
  mockCompiledModule,
} = require('./helpers/fake-browser.js');

function loadMainWithEnvironment(environment, sendToServer) {
  const mainPath = require.resolve('../dist-test/main.js');
  const pageFreezerPath = require.resolve('../dist-test/page-freezer.js');
  const distTestPath = path.join(__dirname, '../dist-test');
  const restoreGlobals = installGlobalBindings({
    window: environment.window,
    document: environment.document,
    MutationObserver: environment.MutationObserver,
    location: environment.location,
    history: environment.history,
    navigator: environment.navigator,
    GM_cookie: undefined,
    DateNow: environment.getNow,
  });

  delete require.cache[pageFreezerPath];
  const restoreMocks = [
    mockCompiledModule(path.join(distTestPath, 'config.js'), {
      CONFIG: {
        AUTH_PASSWORD: '',
        DOM_STABILITY_DELAY: 5,
        DOM_STABLE_TIME: 25,
        ENABLE_COMPRESSION: false,
        FRAME_CAPTURE_TIMEOUT: 100,
        FRAME_CONTENT_CHECK_INTERVAL: 10,
        FRAME_CONTENT_WAIT_TIMEOUT: 100,
        FRAME_DOM_STABLE_TIME: 25,
        FRAME_MUTATION_OBSERVER_TIMEOUT: 100,
        MUTATION_OBSERVER_TIMEOUT: 100,
        REQUEST_TIMEOUT: 100,
        SERVER_URL: 'http://localhost:8080/api/archive',
        SPA_TRANSITION_DELAY: 5,
        TIMER_CLEAR_RANGE: 100,
        UPDATE_CHECK_INTERVAL: 100,
        UPDATE_MIN_MUTATIONS: 10,
        UPDATE_MONITOR_TIMEOUT: 1000,
      },
    }),
    mockCompiledModule(path.join(distTestPath, 'page-filter.js'), {
      shouldSkipPage: () => false,
    }),
    mockCompiledModule(path.join(distTestPath, 'archiver.js'), {
      sendToServer,
      updateOnServer: async () => ({ action: 'updated', page_id: 1, status: 'success' }),
    }),
    mockCompiledModule(path.join(distTestPath, 'dom-collector.js'), {
      DOMCollector: class {
        constructor() {
          this.collectedCount = 0;
          this.reachedLimit = false;
        }

        clear() {}

        handleMutations() {}

        mergeInto(html) {
          return html;
        }
      },
    }),
    mockCompiledModule(path.join(distTestPath, 'frame-capture.js'), {
      captureDocumentHTMLWithFrames: async () => ({
        frames: [],
        html: '<html><body>quiet page</body></html>',
      }),
      setupFrameCaptureBridge: () => {},
    }),
  ];

  delete require.cache[mainPath];
  require(mainPath);

  return {
    restore() {
      delete require.cache[mainPath];
      delete require.cache[pageFreezerPath];
      for (const restoreMock of restoreMocks.reverse()) {
        restoreMock();
      }
      restoreGlobals();
    },
  };
}

test('main starts the initial capture on a quiet page after stableTime instead of waiting for timeout', async () => {
  const environment = createFakeBrowserEnvironment();
  const sendCalls = [];
  const { restore } = loadMainWithEnvironment(environment, async (captureData) => {
    sendCalls.push(captureData);
    return { action: 'created', page_id: 1, status: 'success' };
  });

  try {
    environment.advanceTime(5);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 0, 'capture should not send before DOM_STABLE_TIME elapses');

    environment.advanceTime(24);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 0, 'quiet pages should still wait for the full stable window');

    environment.advanceTime(1);
    await flushMicrotasks();

    assert.equal(sendCalls.length, 1, 'initial capture should be sent as soon as the quiet page is stable');
    assert.equal(sendCalls[0].url, 'https://example.com/articles/quiet-page');
    assert.equal(sendCalls[0].title, 'Quiet page');
    assert.match(sendCalls[0].html, /quiet page/);
  } finally {
    restore();
  }
});

test('main archives a short quiet-page visit once before pagehide', async () => {
  const environment = createFakeBrowserEnvironment();
  const sendCalls = [];
  const { restore } = loadMainWithEnvironment(environment, async (captureData) => {
    sendCalls.push(captureData);
    return { action: 'created', page_id: 1, status: 'success' };
  });

  try {
    environment.advanceTime(5);
    await flushMicrotasks();

    environment.advanceTime(24);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 0, 'short visits should not archive before the page is actually stable');

    environment.advanceTime(1);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 1, 'quiet pages should archive before a user leaves well before the hard timeout');

    environment.window.dispatchEvent({ type: 'pagehide' });
    await flushMicrotasks();
    assert.equal(sendCalls.length, 1, 'pagehide should not duplicate the initial archive send');
  } finally {
    restore();
  }
});

test('main archives a short quiet-page visit once before visibilitychange hidden', async () => {
  const environment = createFakeBrowserEnvironment();
  const sendCalls = [];
  const { restore } = loadMainWithEnvironment(environment, async (captureData) => {
    sendCalls.push(captureData);
    return { action: 'created', page_id: 1, status: 'success' };
  });

  try {
    environment.advanceTime(5);
    await flushMicrotasks();

    environment.advanceTime(24);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 0, 'hidden flush should not fire before the page is actually stable');

    environment.advanceTime(1);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 1, 'quiet pages should archive before a user hides the tab well before the hard timeout');

    environment.document.visibilityState = 'hidden';
    environment.document.dispatchEvent({ type: 'visibilitychange' });
    await flushMicrotasks();
    assert.equal(sendCalls.length, 1, 'visibilitychange hidden should not duplicate the initial archive send');
  } finally {
    restore();
  }
});

test('main archives a short quiet-page visit once before beforeunload', async () => {
  const environment = createFakeBrowserEnvironment();
  const sendCalls = [];
  const { restore } = loadMainWithEnvironment(environment, async (captureData) => {
    sendCalls.push(captureData);
    return { action: 'created', page_id: 1, status: 'success' };
  });

  try {
    environment.advanceTime(5);
    await flushMicrotasks();

    environment.advanceTime(24);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 0, 'beforeunload should not fire before the page is actually stable');

    environment.advanceTime(1);
    await flushMicrotasks();
    assert.equal(sendCalls.length, 1, 'quiet pages should archive before a user unloads the page well before the hard timeout');

    environment.window.dispatchEvent({ type: 'beforeunload' });
    await flushMicrotasks();
    assert.equal(sendCalls.length, 1, 'beforeunload should not duplicate the initial archive send');
  } finally {
    restore();
  }
});
