'use strict';

const HOST = 'dev.agent_chrome_bridge';
const CONTENT_SCRIPT_FILE = 'content-bridge.js';
const BRIDGE_VERSION = chrome.runtime.getManifest().version;
let port = null;
let reconnectTimer = null;
let snapshotCounter = 0;
const snapshotCache = new Map();

function connectNative() {
  if (port) return;

  try {
    port = chrome.runtime.connectNative(HOST);
  } catch {
    scheduleReconnect();
    return;
  }

  port.onMessage.addListener((message) => {
    if (message.type === 'response') return;
    dispatch(message).then(
      (result) => port?.postMessage({ id: message.id, result }),
      (error) => port?.postMessage({ id: message.id, error: error.message || String(error) }),
    );
  });

  port.onDisconnect.addListener(() => {
    // Reading runtime.lastError is required: Chrome records an "Unchecked
    // runtime.lastError: Native host has exited" extension error if a native
    // messaging port disconnects and the callback never inspects the error.
    // Reloading/upgrading the connector intentionally tears down the old native
    // host, so consume the diagnostic and reconnect instead of surfacing it as
    // an extension failure.
    const lastError = chrome.runtime.lastError;
    if (lastError && lastError.message) {
      console.warn(`native host disconnected: ${lastError.message}`);
    }
    port = null;
    scheduleReconnect();
  });
}

function scheduleReconnect() {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connectNative();
  }, 1000);
}

async function dispatch(message) {
  const params = message.params || {};
  switch (message.method) {
    case 'bridge.info':
      return bridgeInfo(params);
    case 'tabs.list':
      return tabsList();
    case 'tabs.active':
      return activeTab();
    case 'tabs.select':
      return tabSelect(params);
    case 'tabs.create':
      return tabCreate(params);
    case 'tabs.close':
      return tabClose(params);
    case 'tabs.reload':
      return tabReload(params);
    case 'tabs.back':
      return tabHistory(params, 'back');
    case 'tabs.forward':
      return tabHistory(params, 'forward');
    case 'page.snapshot':
      return pageSnapshot(params);
    case 'page.find':
      return pageFind(params);
    case 'page.extractLinks':
      return pageExtractLinks(params);
    case 'page.extractTables':
      return pageExtractTables(params);
    case 'page.readArticle':
      return pageReadArticle(params);
    case 'page.network':
      return pageNetwork(params);
    case 'page.navigate':
      return pageNavigate(params);
    case 'page.click':
      return pageClick(params);
    case 'page.type':
      return pageType(params);
    case 'page.scroll':
      return pageScroll(params);
    case 'page.waitForSelector':
      return pageWaitForSelector(params);
    case 'page.waitForText':
      return pageWaitForText(params);
    case 'page.waitUntilIdle':
      return pageWaitUntilIdle(params);
    case 'page.screenshot':
      return pageScreenshot(params);
    case 'cdp.health':
      return cdpHealth();
    case 'cdp.click':
      return cdpClick(params);
    case 'cdp.key':
      return cdpKey(params);
    case 'cdp.type':
      return cdpType(params);
    case 'cdp.console':
      return cdpConsole(params);
    default:
      throw new Error(`Unknown bridge method: ${message.method}`);
  }
}

async function bridgeInfo(params = {}) {
  const manifest = chrome.runtime.getManifest();
  const info = {
    extension: {
      id: chrome.runtime.id,
      name: manifest.name,
      version: manifest.version,
      backgroundVersion: BRIDGE_VERSION,
      manifestVersion: manifest.manifest_version,
      features: {
        cdpKeyCharEvent: true,
        nativeDisconnectLastErrorHandled: true,
      },
    },
    contentScript: {
      ok: false,
      error: 'No injectable tab found.',
    },
  };

  const probe = await resolveContentProbeTab(params);
  if (!probe?.tab) return info;

  try {
    const content = await executeContent(probe.tab.id, 'bridgeInfo');
    info.contentScript = {
      ok: true,
      tab: tabSummary(probe.tab),
      selection: probe.selection,
      ...content,
    };
  } catch (error) {
    info.contentScript = {
      ok: false,
      tab: tabSummary(probe.tab),
      selection: probe.selection,
      error: error.message || String(error),
    };
  }

  return info;
}

async function tabsList() {
  const tabs = await chrome.tabs.query({});
  return tabs.map(tabSummary);
}

async function activeTab() {
  const tab = await resolveTab({});
  return tabSummary(tab);
}

async function tabSelect(params) {
  if (params.tabId === undefined || params.tabId === null) throw new Error('tabId is required');
  const tab = await chrome.tabs.update(Number(params.tabId), { active: true });
  await chrome.windows.update(tab.windowId, { focused: true });
  return tabSummary(tab);
}

async function tabCreate(params) {
  const options = { active: params.active !== false };
  if (params.url) options.url = params.url;
  const tab = await chrome.tabs.create(options);
  return tabSummary(tab);
}

async function tabClose(params) {
  if (params.tabId === undefined || params.tabId === null) throw new Error('tabId is required');
  const tab = await chrome.tabs.get(Number(params.tabId));
  await chrome.tabs.remove(tab.id);
  snapshotCache.delete(tab.id);
  return { closed: true, tab: tabSummary(tab) };
}

async function tabReload(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  await chrome.tabs.reload(tab.id, { bypassCache: params.bypassCache === true });
  return { reloaded: true, tab: tabSummary(tab) };
}

async function tabHistory(params, direction) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  await executeContent(tab.id, direction === 'back' ? 'historyBack' : 'historyForward', [], params.frameId);
  return { navigated: direction, tab: tabSummary(tab) };
}

async function resolveTab(params) {
  if (params.tabId !== undefined && params.tabId !== null) {
    return chrome.tabs.get(Number(params.tabId));
  }

  const focused = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (focused[0]) return focused[0];

  const current = await chrome.tabs.query({ active: true, currentWindow: true });
  if (current[0]) return current[0];

  throw new Error('No active Chrome tab found.');
}

function tabSummary(tab) {
  return {
    id: tab.id,
    windowId: tab.windowId,
    active: tab.active,
    highlighted: tab.highlighted,
    pinned: tab.pinned,
    audible: tab.audible,
    status: tab.status,
    title: tab.title,
    url: tab.url,
  };
}

function assertTabGuard(tab, params) {
  if (!params.expectedHost && !params.expectedUrlIncludes) return;

  if (params.expectedUrlIncludes && !String(tab.url || '').includes(params.expectedUrlIncludes)) {
    throw new Error(`Tab guard failed: URL does not include ${params.expectedUrlIncludes}`);
  }

  if (params.expectedHost) {
    let host = '';
    try {
      host = new URL(tab.url || '').host;
    } catch {
      host = '';
    }
    if (host !== params.expectedHost) {
      throw new Error(`Tab guard failed: expected host ${params.expectedHost}, got ${host || '(none)'}`);
    }
  }
}

async function resolveContentProbeTab(params = {}) {
  if (params.tabId !== undefined && params.tabId !== null) {
    return { tab: await chrome.tabs.get(Number(params.tabId)), selection: 'requested' };
  }

  const active = await resolveTab({});
  if (isInjectableUrl(active.url)) return { tab: active, selection: 'active' };

  const tabs = await chrome.tabs.query({});
  const fallback = tabs.find((tab) => isInjectableUrl(tab.url));
  if (fallback) return { tab: fallback, selection: 'firstInjectable' };

  return { tab: active, selection: 'activeNonInjectable' };
}

function isInjectableUrl(url) {
  return /^(https?:|file:)/i.test(String(url || ''));
}

function numberParam(params, name, fallback) {
  const value = params[name];
  if (value === undefined || value === null || value === '') return fallback;
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function clampedNumberParam(params, name, fallback, min, max) {
  return Math.max(min, Math.min(max, numberParam(params, name, fallback)));
}

async function executeContent(tabId, method, args = [], frameId = undefined) {
  const target = frameId === undefined || frameId === null
    ? { tabId }
    : { tabId, frameIds: [Number(frameId)] };
  await chrome.scripting.executeScript({ target, files: [CONTENT_SCRIPT_FILE] });
  const results = await chrome.scripting.executeScript({
    target,
    func: (methodName, methodArgs) => globalThis.__agentChromeBridge[methodName](...(methodArgs || [])),
    args: [method, args],
  });
  return results?.[0]?.result;
}

async function executeContentAllFrames(tabId, method, args = []) {
  await chrome.scripting.executeScript({
    target: { tabId, allFrames: true },
    files: [CONTENT_SCRIPT_FILE],
  });
  const results = await chrome.scripting.executeScript({
    target: { tabId, allFrames: true },
    func: (methodName, methodArgs) => globalThis.__agentChromeBridge[methodName](...(methodArgs || [])),
    args: [method, args],
  });
  return results
    .filter((entry) => entry?.result)
    .map((entry) => ({ frameId: entry.frameId || 0, result: entry.result }));
}

async function runContent(tabId, method, args, params) {
  if (params.frameId !== undefined && params.frameId !== null) {
    return [{ frameId: Number(params.frameId), result: await executeContent(tabId, method, args, params.frameId) }];
  }
  if (params.includeFrames === false) {
    return [{ frameId: 0, result: await executeContent(tabId, method, args, 0) }];
  }
  return executeContentAllFrames(tabId, method, args);
}

function combineFrameResults(frames, options = {}) {
  const main = frames.find((frame) => frame.frameId === 0)?.result || frames[0]?.result || {};
  const elements = frames.flatMap((frame) => (frame.result.elements || []).map((element) => ({
    ...element,
    frameId: frame.frameId,
    frameUrl: frame.result.url,
    frameTitle: frame.result.title,
  })));
  const text = frames
    .map((frame) => frame.result.text)
    .filter(Boolean)
    .join('\n\n');

  return {
    ...main,
    text: text || main.text || '',
    elements,
    frames: frames.map((frame) => ({
      frameId: frame.frameId,
      title: frame.result.title,
      url: frame.result.url,
      readyState: frame.result.readyState,
      elementCount: frame.result.elements?.length || 0,
    })),
    frameCount: frames.length,
    includeFrames: options.includeFrames !== false,
  };
}

function combineListFrames(frames, key) {
  const main = frames.find((frame) => frame.frameId === 0)?.result || frames[0]?.result || {};
  const items = frames.flatMap((frame) => (frame.result[key] || []).map((item) => ({
    ...item,
    frameId: frame.frameId,
    frameUrl: frame.result.url,
    frameTitle: frame.result.title,
  })));
  return {
    title: main.title,
    url: main.url,
    [key]: items,
    frames: frames.map((frame) => ({ frameId: frame.frameId, title: frame.result.title, url: frame.result.url })),
    frameCount: frames.length,
  };
}

function cacheElements(tabId, snapshot, prefix) {
  const snapshotId = `snapshot-${Date.now()}-${++snapshotCounter}`;
  const elements = new Map();
  const enriched = (snapshot.elements || []).map((element, index) => {
    const id = `${prefix}${index + 1}`;
    const next = { id, ...element };
    elements.set(id, next);
    return next;
  });

  const cached = {
    snapshotId,
    cachedAt: new Date().toISOString(),
    elements,
  };
  snapshotCache.set(tabId, cached);

  return {
    ...snapshot,
    snapshotId,
    cachedAt: cached.cachedAt,
    elements: enriched,
  };
}

function targetFromCache(tabId, elementId) {
  const cache = snapshotCache.get(tabId);
  const element = cache?.elements?.get(elementId);
  if (!element) {
    throw new Error(`Unknown elementId ${elementId}. Run chrome_snapshot or chrome_find for this tab first.`);
  }
  return {
    selector: element.selector,
    elementId,
    frameId: element.frameId,
    ref: element.ref,
  };
}

async function resolveClickTarget(tabId, params) {
  if (params.selector) return { selector: params.selector, frameId: params.frameId };
  if (params.elementId) return targetFromCache(tabId, params.elementId);
  if (params.text) {
    const frames = await runContent(tabId, 'findElements', [{
      text: params.text,
      exact: params.exact === true,
      role: params.role,
      limit: Number(params.index || 0) + 1,
      includeShadow: params.includeShadow !== false,
    }], params);
    const combined = combineFrameResults(frames, params);
    const match = combined.elements[Number(params.index || 0)];
    if (!match) throw new Error(`No clickable element found for text: ${params.text}`);
    return { ref: match.ref, selector: match.selector, frameId: match.frameId };
  }
  throw new Error('selector, elementId, or text is required');
}

async function resolveTypeTarget(tabId, params) {
  if (params.selector) return { selector: params.selector, frameId: params.frameId };
  if (params.elementId) return targetFromCache(tabId, params.elementId);
  if (params.targetText || params.placeholder || params.name) {
    const frames = await runContent(tabId, 'findTypeableElements', [{
      text: params.targetText,
      exact: params.exact === true,
      placeholder: params.placeholder,
      name: params.name,
      limit: Number(params.index || 0) + 1,
      includeShadow: params.includeShadow !== false,
    }], params);
    const combined = combineFrameResults(frames, params);
    const match = combined.elements[Number(params.index || 0)];
    if (!match) throw new Error('No typeable element found for target');
    return { ref: match.ref, selector: match.selector, frameId: match.frameId };
  }
  throw new Error('selector, elementId, targetText, placeholder, or name is required');
}

async function pageSnapshot(params) {
  const tab = await resolveTab(params);
  const frames = await runContent(tab.id, 'snapshotPage', [{
    maxText: clampedNumberParam(params, 'maxText', 12000, 0, 50000),
    maxElements: clampedNumberParam(params, 'maxElements', 120, 0, 500),
    includeShadow: params.includeShadow !== false,
  }], params);
  const snapshot = cacheElements(tab.id, combineFrameResults(frames, params), 'e');
  return { tab: tabSummary(tab), snapshot };
}

async function pageFind(params) {
  const tab = await resolveTab(params);
  const frames = await runContent(tab.id, 'findElements', [{
    selector: params.selector,
    text: params.text,
    exact: params.exact === true,
    role: params.role,
    tag: params.tag,
    placeholder: params.placeholder,
    name: params.name,
    href: params.href,
    limit: Number(params.limit || 20),
    includeShadow: params.includeShadow !== false,
  }], params);
  const result = cacheElements(tab.id, combineFrameResults(frames, params), 'f');
  return { tab: tabSummary(tab), result };
}

async function pageExtractLinks(params) {
  const tab = await resolveTab(params);
  const frames = await runContent(tab.id, 'extractLinks', [{
    text: params.text,
    exact: params.exact === true,
    href: params.href,
    limit: Number(params.limit || 200),
    visible: params.visible !== false,
    includeShadow: params.includeShadow !== false,
  }], params);
  return { tab: tabSummary(tab), result: combineListFrames(frames, 'links') };
}

async function pageExtractTables(params) {
  const tab = await resolveTab(params);
  const frames = await runContent(tab.id, 'extractTables', [{
    limit: Number(params.limit || 20),
    maxRows: Number(params.maxRows || 50),
    visible: params.visible !== false,
    includeShadow: params.includeShadow !== false,
  }], params);
  return { tab: tabSummary(tab), result: combineListFrames(frames, 'tables') };
}

async function pageReadArticle(params) {
  const tab = await resolveTab(params);
  const frames = await runContent(tab.id, 'readArticle', [{
    maxText: clampedNumberParam(params, 'maxText', 30000, 0, 100000),
    includeShadow: params.includeShadow !== false,
  }], { ...params, includeFrames: params.includeFrames === true });
  const articles = frames.map((frame) => ({ frameId: frame.frameId, ...frame.result }));
  const best = articles.slice().sort((left, right) => (right.textLength || 0) - (left.textLength || 0))[0] || null;
  return { tab: tabSummary(tab), article: best, frames: articles.map((article) => ({ frameId: article.frameId, title: article.title, url: article.url, textLength: article.textLength })) };
}

async function pageNetwork(params) {
  const tab = await resolveTab(params);
  const frames = await runContent(tab.id, 'getNetworkEntries', [{
    limit: Number(params.limit || 200),
  }], params);
  return { tab: tabSummary(tab), result: combineListFrames(frames, 'entries') };
}

async function pageNavigate(params) {
  if (!params.url) throw new Error('url is required');
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const updated = await chrome.tabs.update(tab.id, { url: params.url });
  return tabSummary(updated);
}

async function pageClick(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const target = await resolveClickTarget(tab.id, params);
  const result = await executeContent(tab.id, 'clickTarget', [target], target.frameId);
  return { tab: tabSummary(tab), ...result };
}

async function pageType(params) {
  if (typeof params.text !== 'string') throw new Error('text is required');
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const target = await resolveTypeTarget(tab.id, params);
  const result = await executeContent(tab.id, 'typeIntoTarget', [
    target,
    params.text,
    params.replace !== false,
    params.submit === true,
  ], target.frameId);
  return { tab: tabSummary(tab), ...result };
}

async function pageScroll(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const result = await executeContent(tab.id, 'scrollPage', [
    Number(params.x || 0),
    Number(params.y === undefined ? 700 : params.y),
  ], params.frameId);
  return { tab: tabSummary(tab), ...result };
}

async function pageWaitForSelector(params) {
  if (!params.selector) throw new Error('selector is required');
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const result = await executeContent(tab.id, 'waitForSelectorInPage', [
    params.selector,
    params.visible !== false,
    Number(params.timeoutMs || 10000),
  ], params.frameId);
  return { tab: tabSummary(tab), ...result };
}

async function pageWaitForText(params) {
  if (!params.text) throw new Error('text is required');
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const result = await executeContent(tab.id, 'waitForTextInPage', [
    params.text,
    params.exact === true,
    Number(params.timeoutMs || 10000),
  ], params.frameId);
  return { tab: tabSummary(tab), ...result };
}

async function pageWaitUntilIdle(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const result = await executeContent(tab.id, 'waitUntilIdleInPage', [
    Number(params.idleMs || 500),
    Number(params.timeoutMs || 10000),
  ], params.frameId);
  return { tab: tabSummary(tab), ...result };
}

async function pageScreenshot(params) {
  const tab = await resolveTab(params);
  const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, { format: 'png' });
  return {
    tab: tabSummary(tab),
    mimeType: 'image/png',
    dataUrl,
  };
}

function cdpHealth() {
  return {
    debuggerApiAvailable: Boolean(chrome.debugger),
    note: chrome.debugger
      ? 'CDP tools can attach to a tab for one command and then detach.'
      : 'Chrome debugger API is unavailable. Reload the extension after granting the debugger permission.',
  };
}

async function withDebugger(tabId, callback, waitMs = 0) {
  if (!chrome.debugger) throw new Error('Chrome debugger API is unavailable. Reload the extension after granting debugger permission.');
  const target = { tabId };
  let attached = false;
  try {
    await chrome.debugger.attach(target, '1.3');
    attached = true;
    const result = await callback(target);
    if (waitMs > 0) await delay(waitMs);
    return result;
  } finally {
    if (attached) {
      try {
        await chrome.debugger.detach(target);
      } catch {
        // The tab may have navigated or closed during the command.
      }
    }
  }
}

async function cdpSend(target, method, params = {}) {
  return chrome.debugger.sendCommand(target, method, params);
}

async function cdpClick(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  if (params.x === undefined || params.y === undefined) throw new Error('x and y are required');
  const x = Number(params.x);
  const y = Number(params.y);
  const button = params.button || 'left';
  const clickCount = Number(params.clickCount || 1);
  await withDebugger(tab.id, async (target) => {
    await cdpSend(target, 'Input.dispatchMouseEvent', { type: 'mouseMoved', x, y, button: 'none' });
    await cdpSend(target, 'Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button, clickCount });
    await cdpSend(target, 'Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button, clickCount });
  });
  return { tab: tabSummary(tab), clicked: true, x, y, button, clickCount };
}

// CDP Input.dispatchKeyEvent modifier bitmask: Alt=1, Ctrl=2, Meta/Cmd=4, Shift=8
const CDP_MODIFIER_BITS = {
  alt: 1,
  ctrl: 2,
  control: 2,
  meta: 4,
  cmd: 4,
  command: 4,
  super: 4,
  win: 4,
  shift: 8,
};

function resolveCdpModifiers(params) {
  let bits = 0;
  if (params.modifiers !== undefined && params.modifiers !== null) {
    const n = Number(params.modifiers);
    if (!Number.isFinite(n) || n < 0) throw new Error('modifiers must be a non-negative number');
    bits |= n & 0xf;
  }
  if (Array.isArray(params.modifierKeys)) {
    for (const raw of params.modifierKeys) {
      if (raw == null) continue;
      const key = String(raw).toLowerCase().trim();
      if (!key) continue;
      const bit = CDP_MODIFIER_BITS[key];
      if (bit === undefined) throw new Error(`unknown modifier key: ${raw}`);
      bits |= bit;
    }
  }
  return bits;
}

function cdpKeyDescriptor(params, modifiers) {
  const rawKey = String(params.key);
  const alias = rawKey.toLowerCase().trim();
  let key = rawKey;
  let code = params.code || rawKey;
  let text = '';
  let windowsVirtualKeyCode = 0;

  if (rawKey === ' ' || alias === 'space' || alias === 'spacebar') {
    key = ' ';
    code = params.code || 'Space';
    text = ' ';
    windowsVirtualKeyCode = 32;
  } else if ([...rawKey].length === 1 && rawKey >= ' ' && rawKey !== '\u007f') {
    text = rawKey;
    const upper = rawKey.toUpperCase();
    if (/^[A-Z]$/.test(upper)) {
      code = params.code || `Key${upper}`;
      windowsVirtualKeyCode = upper.charCodeAt(0);
    } else if (/^[0-9]$/.test(rawKey)) {
      code = params.code || `Digit${rawKey}`;
      windowsVirtualKeyCode = rawKey.charCodeAt(0);
    }
  } else {
    const named = {
      enter: ['Enter', 'Enter', 13],
      return: ['Enter', 'Enter', 13],
      tab: ['Tab', 'Tab', 9],
      escape: ['Escape', 'Escape', 27],
      esc: ['Escape', 'Escape', 27],
      backspace: ['Backspace', 'Backspace', 8],
      delete: ['Delete', 'Delete', 46],
      arrowleft: ['ArrowLeft', 'ArrowLeft', 37],
      left: ['ArrowLeft', 'ArrowLeft', 37],
      arrowup: ['ArrowUp', 'ArrowUp', 38],
      up: ['ArrowUp', 'ArrowUp', 38],
      arrowright: ['ArrowRight', 'ArrowRight', 39],
      right: ['ArrowRight', 'ArrowRight', 39],
      arrowdown: ['ArrowDown', 'ArrowDown', 40],
      down: ['ArrowDown', 'ArrowDown', 40],
    }[alias];
    if (named) {
      [key, code, windowsVirtualKeyCode] = named;
      code = params.code || code;
    }
  }

  // Printable characters should produce a CDP `char` event in addition to the
  // keydown/up pair. Without this, focused text editors receive a Space keydown
  // but do not insert an actual space, which made browser notepad-style pages
  // collapse "a b" into "ab".
  if (modifiers & (CDP_MODIFIER_BITS.ctrl | CDP_MODIFIER_BITS.meta | CDP_MODIFIER_BITS.alt)) {
    text = '';
  }

  const event = { key, code, modifiers };
  if (windowsVirtualKeyCode) {
    event.windowsVirtualKeyCode = windowsVirtualKeyCode;
    event.nativeVirtualKeyCode = windowsVirtualKeyCode;
  }
  return { event, text, key, code };
}

async function cdpKey(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  if (!params.key) throw new Error('key is required');
  const modifiers = resolveCdpModifiers(params);
  const descriptor = cdpKeyDescriptor(params, modifiers);
  await withDebugger(tab.id, async (target) => {
    await cdpSend(target, 'Input.dispatchKeyEvent', { ...descriptor.event, type: 'rawKeyDown' });
    if (descriptor.text) {
      await cdpSend(target, 'Input.dispatchKeyEvent', {
        ...descriptor.event,
        type: 'char',
        text: descriptor.text,
        unmodifiedText: descriptor.text,
      });
    }
    await cdpSend(target, 'Input.dispatchKeyEvent', { ...descriptor.event, type: 'keyUp' });
  });
  return { tab: tabSummary(tab), pressed: true, key: descriptor.key, code: descriptor.code, modifiers };
}

async function cdpType(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  if (typeof params.text !== 'string') throw new Error('text is required');
  await withDebugger(tab.id, async (target) => {
    await cdpSend(target, 'Input.insertText', { text: params.text });
  });
  return { tab: tabSummary(tab), typed: true, length: params.text.length };
}

async function cdpConsole(params) {
  const tab = await resolveTab(params);
  assertTabGuard(tab, params);
  const waitMs = clampedNumberParam(params, 'waitMs', 1000, 0, 10000);
  const entries = [];

  await withDebugger(tab.id, async (target) => {
    const listener = (source, method, eventParams) => {
      if (source.tabId !== tab.id) return;
      if (method === 'Runtime.consoleAPICalled') {
        entries.push({
          type: eventParams.type,
          timestamp: eventParams.timestamp,
          args: (eventParams.args || []).map(remoteObjectSummary),
        });
      } else if (method === 'Runtime.exceptionThrown') {
        entries.push({
          type: 'exception',
          timestamp: eventParams.timestamp,
          text: eventParams.exceptionDetails?.text,
          url: eventParams.exceptionDetails?.url,
          lineNumber: eventParams.exceptionDetails?.lineNumber,
          columnNumber: eventParams.exceptionDetails?.columnNumber,
        });
      } else if (method === 'Log.entryAdded') {
        entries.push({
          type: eventParams.entry?.level || 'log',
          source: eventParams.entry?.source,
          text: eventParams.entry?.text,
          url: eventParams.entry?.url,
          lineNumber: eventParams.entry?.lineNumber,
        });
      }
    };

    chrome.debugger.onEvent.addListener(listener);
    try {
      await cdpSend(target, 'Runtime.enable');
      await cdpSend(target, 'Log.enable');
      await cdpSend(target, 'Runtime.evaluate', { expression: 'undefined' });
      await delay(waitMs);
    } finally {
      chrome.debugger.onEvent.removeListener(listener);
    }
  });

  return { tab: tabSummary(tab), waitMs, entries };
}

function remoteObjectSummary(value) {
  if ('value' in value) return value.value;
  if (value.unserializableValue) return value.unserializableValue;
  if (value.description) return value.description;
  return value.type;
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

chrome.runtime.onStartup.addListener(connectNative);
chrome.runtime.onInstalled.addListener(connectNative);
connectNative();
