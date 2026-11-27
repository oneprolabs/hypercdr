import assert from 'node:assert/strict';
import test from 'node:test';
import { followTimeline } from './timeline-scroll.ts';

test('timeline follows growth, respects manual scrolling, resumes at bottom and cleans up', () => {
  const originals = Object.fromEntries(['getComputedStyle', 'ResizeObserver', 'requestAnimationFrame', 'cancelAnimationFrame'].map(key => [key, globalThis[key]]));
  let resized;
  let scrolled;
  let frame;
  let disconnected = false;
  const viewport = {
    parentElement: null, overflowY: 'auto', scrollHeight: 500, clientHeight: 100, scrollTop: 0,
    addEventListener: (_name, callback) => { scrolled = callback; },
    removeEventListener: (_name, callback) => { assert.equal(callback, scrolled); scrolled = undefined; },
  };
  globalThis.getComputedStyle = element => element;
  globalThis.ResizeObserver = class {
    constructor(callback) { resized = callback; }
    observe() {}
    disconnect() { disconnected = true; }
  };
  globalThis.requestAnimationFrame = callback => { frame = callback; return 1; };
  globalThis.cancelAnimationFrame = () => { frame = undefined; };
  try {
    const cleanup = followTimeline({ parentElement: { parentElement: viewport, overflowY: 'visible' } });
    frame();
    assert.equal(viewport.scrollTop, 500);
    viewport.scrollTop = 150;
    scrolled();
    frame = undefined;
    viewport.scrollHeight = 700;
    resized();
    assert.equal(frame, undefined);
    assert.equal(viewport.scrollTop, 150);
    viewport.scrollTop = 600;
    scrolled();
    resized();
    viewport.scrollHeight = 800;
    frame();
    assert.equal(viewport.scrollTop, 800);
    cleanup();
    assert.equal(disconnected, true);
    assert.equal(scrolled, undefined);
    assert.equal(frame, undefined);
  } finally {
    for (const [key, value] of Object.entries(originals)) {
      if (value === undefined) delete globalThis[key];
      else globalThis[key] = value;
    }
  }
});
