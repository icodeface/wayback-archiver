function createFakeTimers() {
  let now = 0;
  let nextTimerId = 1;
  const timers = new Map();

  function schedule(callback, delay = 0, interval = null) {
    const id = nextTimerId++;
    timers.set(id, {
      callback,
      interval,
      runAt: now + delay,
    });
    return id;
  }

  function setTimeout(callback, delay = 0) {
    return schedule(callback, delay, null);
  }

  function clearTimeout(id) {
    timers.delete(id);
  }

  function setInterval(callback, delay = 0) {
    return schedule(callback, delay, delay);
  }

  function clearInterval(id) {
    timers.delete(id);
  }

  function advanceTime(ms) {
    const targetTime = now + ms;

    while (true) {
      let nextEntry = null;

      for (const [id, timer] of timers) {
        if (timer.runAt > targetTime) {
          continue;
        }

        if (!nextEntry || timer.runAt < nextEntry.runAt || (timer.runAt === nextEntry.runAt && id < nextEntry.id)) {
          nextEntry = { id, ...timer };
        }
      }

      if (!nextEntry) {
        break;
      }

      now = nextEntry.runAt;

      if (nextEntry.interval === null) {
        timers.delete(nextEntry.id);
      } else {
        timers.set(nextEntry.id, {
          callback: nextEntry.callback,
          interval: nextEntry.interval,
          runAt: nextEntry.runAt + nextEntry.interval,
        });
      }

      nextEntry.callback();
    }

    now = targetTime;
  }

  return {
    advanceTime,
    clearInterval,
    clearTimeout,
    getNow: () => now,
    setInterval,
    setTimeout,
  };
}

function createFakeMutationObserverClass() {
  return class FakeMutationObserver {
    static instances = [];

    constructor(callback) {
      this.callback = callback;
      this.disconnected = false;
      FakeMutationObserver.instances.push(this);
    }

    observe(target, options) {
      this.target = target;
      this.options = options;
    }

    disconnect() {
      this.disconnected = true;
    }

    trigger(records = [{}]) {
      if (!this.disconnected) {
        this.callback(records, this);
      }
    }
  };
}

function createEventTarget() {
  const listeners = new Map();

  return {
    addEventListener(type, handler) {
      const handlers = listeners.get(type) || [];
      handlers.push(handler);
      listeners.set(type, handlers);
    },
    dispatchEvent(event) {
      const handlers = listeners.get(event.type) || [];
      for (const handler of handlers) {
        handler(event);
      }
    },
    removeEventListener(type, handler) {
      const handlers = listeners.get(type) || [];
      listeners.set(type, handlers.filter((current) => current !== handler));
    },
  };
}

function createFakeDOMEnvironment() {
  const timers = createFakeTimers();
  const MutationObserver = createFakeMutationObserverClass();

  return {
    advanceTime: timers.advanceTime,
    document: {
      body: {},
    },
    getNow: timers.getNow,
    MutationObserver,
    window: {
      clearTimeout: timers.clearTimeout,
      setTimeout: timers.setTimeout,
    },
  };
}

function createFakeBrowserEnvironment() {
  const timers = createFakeTimers();
  const windowEvents = createEventTarget();
  const documentEvents = createEventTarget();
  const location = {
    href: 'https://example.com/articles/quiet-page',
    hostname: 'example.com',
  };

  const MutationObserver = createFakeMutationObserverClass();
  const document = {
    ...documentEvents,
    body: {},
    title: 'Quiet page',
    visibilityState: 'visible',
  };

  const windowObject = {
    ...windowEvents,
    clearInterval: timers.clearInterval,
    clearTimeout: timers.clearTimeout,
    location,
    requestAnimationFrame: () => 0,
    scrollY: 0,
    setInterval: timers.setInterval,
    setTimeout: timers.setTimeout,
  };
  windowObject.self = windowObject;
  windowObject.top = windowObject;

  return {
    MutationObserver,
    advanceTime: timers.advanceTime,
    document,
    getNow: timers.getNow,
    history: {
      pushState() {},
      replaceState() {},
    },
    location,
    navigator: {
      userAgent: 'test-agent',
    },
    window: windowObject,
  };
}

async function flushMicrotasks(times = 6) {
  for (let i = 0; i < times; i += 1) {
    await Promise.resolve();
  }
}

function installGlobalBindings(bindings) {
  const originalDateNow = Date.now;
  const originalGlobals = new Map();

  for (const [name, value] of Object.entries(bindings)) {
    if (name === 'DateNow') {
      continue;
    }

    originalGlobals.set(name, global[name]);
    if (value === undefined) {
      delete global[name];
    } else {
      global[name] = value;
    }
  }

  if (bindings.DateNow) {
    Date.now = bindings.DateNow;
  }

  return () => {
    for (const [name, value] of originalGlobals.entries()) {
      if (value === undefined) {
        delete global[name];
      } else {
        global[name] = value;
      }
    }

    Date.now = originalDateNow;
  };
}

function mockCompiledModule(relativePath, exports) {
  const resolvedPath = require.resolve(relativePath);
  const originalModule = require.cache[resolvedPath];

  require.cache[resolvedPath] = {
    exports,
    filename: resolvedPath,
    id: resolvedPath,
    loaded: true,
  };

  return () => {
    if (originalModule) {
      require.cache[resolvedPath] = originalModule;
      return;
    }

    delete require.cache[resolvedPath];
  };
}

module.exports = {
  createFakeBrowserEnvironment,
  createFakeDOMEnvironment,
  flushMicrotasks,
  installGlobalBindings,
  mockCompiledModule,
};
