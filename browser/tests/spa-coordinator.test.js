const test = require('node:test');
const assert = require('node:assert/strict');

const {
  chooseFlushAction,
} = require('../dist-test/spa-coordinator.js');

test('unload flush does nothing when initial capture data was never prepared', () => {
  const action = chooseFlushAction({
    capturePrepared: false,
    hasArchived: false,
    sendInFlight: false,
    updateInFlight: false,
    documentHidden: false,
    currentPageId: null,
  });

  assert.equal(action, 'none');
});

test('unload flush sends the initial capture once data is ready', () => {
  const action = chooseFlushAction({
    capturePrepared: true,
    hasArchived: false,
    sendInFlight: false,
    updateInFlight: false,
    documentHidden: false,
    currentPageId: null,
  });

  assert.equal(action, 'send-capture');
});
