const test = require('node:test');
const assert = require('node:assert/strict');

const {
  chooseFlushAction,
  choosePendingFlushDependency,
  shouldClearAsyncState,
  shouldCommitMonitorUpdate,
} = require('../dist-test/spa-coordinator.js');

test('chooseFlushAction sends initial capture before first archive', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: false,
    sendInFlight: false,
    currentPageId: null,
  }), 'send-capture');
});

test('chooseFlushAction flushes current page with update after initial archive', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: true,
    sendInFlight: false,
    currentPageId: 42,
  }), 'update-current-page');
});

test('chooseFlushAction does nothing when archived page has no page id yet', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: true,
    sendInFlight: false,
    currentPageId: null,
  }), 'none');
});

test('chooseFlushAction does not start a second flush while send is in flight', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: false,
    sendInFlight: true,
    currentPageId: null,
  }), 'none');
});

test('chooseFlushAction skips final flush when document is hidden', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: true,
    sendInFlight: false,
    currentPageId: 42,
    documentHidden: true,
  }), 'none');
});

test('chooseFlushAction still sends initial capture when page hides before first archive', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: false,
    sendInFlight: false,
    currentPageId: null,
    documentHidden: true,
  }), 'send-capture');
});

test('chooseFlushAction does not start a second update while final flush is in flight', () => {
  assert.equal(chooseFlushAction({
    capturePrepared: true,
    hasArchived: true,
    sendInFlight: false,
    currentPageId: 42,
    updateInFlight: true,
  }), 'none');
});

test('choosePendingFlushDependency waits for in-flight initial send', () => {
  assert.equal(choosePendingFlushDependency({
    sendInFlight: true,
    updateInFlight: false,
  }), 'send');
});

test('choosePendingFlushDependency does not wait for in-flight page update during navigation reset', () => {
  assert.equal(choosePendingFlushDependency({
    sendInFlight: false,
    updateInFlight: true,
  }), 'none');
});

test('shouldCommitMonitorUpdate rejects stale async work after SPA reset', () => {
  assert.equal(shouldCommitMonitorUpdate(3, 4, 42, 42), false);
});

test('shouldCommitMonitorUpdate rejects updates when current page id changed', () => {
  assert.equal(shouldCommitMonitorUpdate(3, 3, 42, 43), false);
});

test('shouldCommitMonitorUpdate allows current async work for same page epoch', () => {
  assert.equal(shouldCommitMonitorUpdate(3, 3, 42, 42), true);
});

test('shouldClearAsyncState only clears the currently tracked promise', async () => {
  const settledPromise = Promise.resolve();
  const newerPromise = new Promise(() => {});

  assert.equal(shouldClearAsyncState(settledPromise, settledPromise), true);
  assert.equal(shouldClearAsyncState(newerPromise, settledPromise), false);
});
