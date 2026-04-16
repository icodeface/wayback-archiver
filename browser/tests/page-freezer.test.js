const test = require('node:test');
const assert = require('node:assert/strict');
const {
  createFakeDOMEnvironment,
  flushMicrotasks,
  installGlobalBindings,
} = require('./helpers/fake-browser.js');

function loadPageFreezer(environment) {
  const pageFreezerPath = require.resolve('../dist-test/page-freezer.js');
  const restoreGlobals = installGlobalBindings({
    window: environment.window,
    document: environment.document,
    MutationObserver: environment.MutationObserver,
    DateNow: environment.getNow,
  });

  delete require.cache[pageFreezerPath];
  const pageFreezer = require(pageFreezerPath);

  return {
    pageFreezer,
    restore() {
      delete require.cache[pageFreezerPath];
      restoreGlobals();
    },
  };
}

test('waitForDOMStable resolves after stableTime on a quiet page', async () => {
  const environment = createFakeDOMEnvironment();
  const { pageFreezer, restore } = loadPageFreezer(environment);

  try {
    let settled = false;
    const waitPromise = pageFreezer.waitForDOMStable(100, 25).then(() => {
      settled = true;
    });

    assert.equal(environment.MutationObserver.instances.length, 1);

    environment.advanceTime(24);
    await flushMicrotasks();
    assert.equal(settled, false, 'promise should stay pending until stableTime has fully elapsed');

    environment.advanceTime(1);
    await waitPromise;
    assert.equal(settled, true);
  } finally {
    restore();
  }
});

test('waitForDOMStable only starts the stability timer after the first mutation', async () => {
  const environment = createFakeDOMEnvironment();
  const { pageFreezer, restore } = loadPageFreezer(environment);

  try {
    let settled = false;
    const waitPromise = pageFreezer.waitForDOMStable(100, 25).then(() => {
      settled = true;
    });

    const observer = environment.MutationObserver.instances[0];
    environment.advanceTime(10);
    observer.trigger([{ type: 'childList' }]);
    await flushMicrotasks();

    environment.advanceTime(24);
    await flushMicrotasks();
    assert.equal(settled, false);

    environment.advanceTime(1);
    await waitPromise;
    assert.equal(settled, true);
  } finally {
    restore();
  }
});
