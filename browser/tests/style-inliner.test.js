const test = require('node:test');
const assert = require('node:assert/strict');

const {
  shouldNormalizeDetachedFixedLayout,
  shouldResetDetachedFixedTransform,
} = require('../dist-test/style-inliner.js');

test('normalizes collapsed fixed containers that are narrower than their content column', () => {
  assert.equal(shouldNormalizeDetachedFixedLayout({
    position: 'fixed',
    elementLeft: 321,
    elementWidth: 168,
    parentLeft: 60,
    parentWidth: 275,
    childWidth: 275,
  }), true);
});

test('normalizes fixed containers detached from their parent even when width is not collapsed', () => {
  assert.equal(shouldNormalizeDetachedFixedLayout({
    position: 'fixed',
    elementLeft: 321,
    elementWidth: 768,
    parentLeft: 60,
    parentWidth: 275,
    childWidth: 275,
  }), true);
});

test('ignores small floating fixed widgets', () => {
  assert.equal(shouldNormalizeDetachedFixedLayout({
    position: 'fixed',
    elementLeft: 1400,
    elementWidth: 56,
    parentLeft: 0,
    parentWidth: 1440,
    childWidth: 56,
  }), false);
});

test('ignores containers when the parent column is narrower than the child', () => {
  assert.equal(shouldNormalizeDetachedFixedLayout({
    position: 'fixed',
    elementLeft: 321,
    elementWidth: 168,
    parentLeft: 60,
    parentWidth: 180,
    childWidth: 275,
  }), false);
});

test('ignores fixed containers that remain aligned with their parent', () => {
  assert.equal(shouldNormalizeDetachedFixedLayout({
    position: 'fixed',
    elementLeft: 60,
    elementWidth: 275,
    parentLeft: 60,
    parentWidth: 275,
    childWidth: 275,
  }), false);
});

test('ignores non-fixed elements', () => {
  assert.equal(shouldNormalizeDetachedFixedLayout({
    position: 'sticky',
    elementLeft: 321,
    elementWidth: 168,
    parentLeft: 60,
    parentWidth: 275,
    childWidth: 275,
  }), false);
});

test('resets translateX transforms on normalized fixed containers', () => {
  assert.equal(shouldResetDetachedFixedTransform('translateX(-50%)'), true);
});

test('resets matrix transforms that only translate on X axis', () => {
  assert.equal(shouldResetDetachedFixedTransform('matrix(1, 0, 0, 1, -384, 0)'), true);
});

test('preserves transforms with vertical translation', () => {
  assert.equal(shouldResetDetachedFixedTransform('translateY(24px)'), false);
  assert.equal(shouldResetDetachedFixedTransform('matrix(1, 0, 0, 1, -384, 24)'), false);
});

test('preserves transforms with scaling', () => {
  assert.equal(shouldResetDetachedFixedTransform('scale(0.95)'), false);
  assert.equal(shouldResetDetachedFixedTransform('matrix(0.95, 0, 0, 0.95, -384, 0)'), false);
});
