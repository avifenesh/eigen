'use strict';

(() => {
  const BRIDGE_VERSION = '0.4.0';

  if (globalThis.__agentChromeBridge?.version === BRIDGE_VERSION) return;

  let refCounter = 0;
  const refElements = new Map();

  function historyBack() {
    history.back();
    return true;
  }

  function historyForward() {
    history.forward();
    return true;
  }

  function bridgeInfo() {
    return {
      version: BRIDGE_VERSION,
      title: document.title,
      url: location.href,
      readyState: document.readyState,
    };
  }

  function numberOption(value, fallback, min, max) {
    const raw = value === undefined || value === null || value === '' ? fallback : value;
    const number = Number(raw);
    const finite = Number.isFinite(number) ? number : fallback;
    return Math.max(min, Math.min(max, finite));
  }

  function snapshotPage(options = {}) {
    const maxText = numberOption(options.maxText, 12000, 0, 50000);
    const maxElements = numberOption(options.maxElements, 120, 0, 500);
    const elements = describeElements({ limit: maxElements, includeShadow: options.includeShadow !== false });

    return {
      title: document.title,
      url: location.href,
      readyState: document.readyState,
      viewport: { width: innerWidth, height: innerHeight },
      scroll: { x: scrollX, y: scrollY },
      selection: String(getSelection() || ''),
      text: (document.body?.innerText || '').slice(0, maxText),
      elements,
    };
  }

  function findElements(options = {}) {
    const elements = describeElements({
      selector: options.selector,
      text: options.text,
      exact: options.exact,
      role: options.role,
      tag: options.tag,
      placeholder: options.placeholder,
      name: options.name,
      href: options.href,
      limit: Math.max(1, Math.min(200, Number(options.limit || 20))),
      includeShadow: options.includeShadow !== false,
    });

    return {
      title: document.title,
      url: location.href,
      readyState: document.readyState,
      viewport: { width: innerWidth, height: innerHeight },
      scroll: { x: scrollX, y: scrollY },
      elements,
    };
  }

  function findTypeableElements(options = {}) {
    const elements = queryAllDeep('input, textarea, select, [contenteditable="true"]', options.includeShadow !== false)
      .filter(visible)
      .filter(typeable)
      .filter((element) => {
        if (options.placeholder && !matchText(element.getAttribute('placeholder') || '', options.placeholder, options.exact)) return false;
        if (options.name && !matchText(element.getAttribute('name') || '', options.name, options.exact)) return false;
        if (options.text && !matchText(searchText(element), options.text, options.exact)) return false;
        return true;
      })
      .slice(0, Math.max(1, Math.min(200, Number(options.limit || 20))))
      .map(describeElement);

    return {
      title: document.title,
      url: location.href,
      readyState: document.readyState,
      viewport: { width: innerWidth, height: innerHeight },
      scroll: { x: scrollX, y: scrollY },
      elements,
    };
  }

  function extractLinks(options = {}) {
    const links = queryAllDeep('a[href], area[href]', options.includeShadow !== false)
      .filter((element) => options.visible === false || visible(element))
      .map((element) => ({
        text: textOf(element),
        href: element.href || element.getAttribute('href') || '',
        title: element.getAttribute('title') || undefined,
        rel: element.getAttribute('rel') || undefined,
        target: element.getAttribute('target') || undefined,
        ref: ensureRef(element),
      }))
      .filter((link) => !options.text || matchText(link.text || link.href, options.text, options.exact))
      .filter((link) => !options.href || String(link.href).toLowerCase().includes(String(options.href).toLowerCase()))
      .slice(0, Math.max(1, Math.min(1000, Number(options.limit || 200))));

    return { title: document.title, url: location.href, links };
  }

  function extractTables(options = {}) {
    const tables = queryAllDeep('table', options.includeShadow !== false)
      .filter((table) => options.visible === false || visible(table))
      .slice(0, Math.max(1, Math.min(50, Number(options.limit || 20))))
      .map((table) => {
        const rows = [...table.rows].slice(0, Math.max(1, Math.min(200, Number(options.maxRows || 50))));
        return {
          caption: normalizeSpace(table.caption?.innerText || table.caption?.textContent || ''),
          ref: ensureRef(table),
          headers: [...table.querySelectorAll('thead th')].map((cell) => normalizeSpace(cell.innerText || cell.textContent || '')),
          rows: rows.map((row) => [...row.cells].map((cell) => normalizeSpace(cell.innerText || cell.textContent || ''))),
        };
      });

    return { title: document.title, url: location.href, tables };
  }

  function readArticle(options = {}) {
    const maxText = numberOption(options.maxText, 30000, 0, 100000);
    const candidates = queryAllDeep('article, main, [role="main"], .markdown-body, .entry-content, .post, .content', options.includeShadow !== false)
      .filter(visible)
      .map((element) => ({ element, text: normalizeSpace(element.innerText || element.textContent || '') }))
      .filter((candidate) => candidate.text.length > 200)
      .sort((left, right) => scoreArticleCandidate(right) - scoreArticleCandidate(left));

    const fallbackText = normalizeSpace(document.body?.innerText || '');
    const best = candidates[0] || { element: document.body, text: fallbackText };
    return {
      title: document.title,
      url: location.href,
      lang: document.documentElement.lang || undefined,
      ref: best.element ? ensureRef(best.element) : undefined,
      text: best.text.slice(0, maxText),
      textLength: best.text.length,
      selection: String(getSelection() || ''),
    };
  }

  function getNetworkEntries(options = {}) {
    const limit = Math.max(1, Math.min(1000, Number(options.limit || 200)));
    const entries = performance.getEntriesByType('resource')
      .slice(-limit)
      .map((entry) => ({
        name: entry.name,
        initiatorType: entry.initiatorType,
        duration: Math.round(entry.duration),
        transferSize: entry.transferSize,
        encodedBodySize: entry.encodedBodySize,
        decodedBodySize: entry.decodedBodySize,
        responseStatus: entry.responseStatus || undefined,
      }));
    return { title: document.title, url: location.href, entries };
  }

  function scoreArticleCandidate(candidate) {
    const tagBoost = candidate.element.localName === 'article' ? 5000 : 0;
    const roleBoost = candidate.element.getAttribute('role') === 'main' ? 2000 : 0;
    return candidate.text.length + tagBoost + roleBoost;
  }

  function describeElements(options = {}) {
    const selector = options.selector || actionableSelector();
    const limit = numberOption(options.limit, 120, 0, 500);
    return queryAllDeep(selector, options.includeShadow !== false)
      .filter(visible)
      .filter((element) => matchesFilters(element, options))
      .slice(0, limit)
      .map(describeElement);
  }

  function actionableSelector() {
    return [
      'a[href]',
      'button',
      'input',
      'textarea',
      'select',
      'label',
      '[role="button"]',
      '[role="link"]',
      '[role="menuitem"]',
      '[role="tab"]',
      '[contenteditable="true"]',
      '[tabindex]:not([tabindex="-1"])',
    ].join(',');
  }

  function queryAllDeep(selector, includeShadow = true) {
    const results = [];
    const seen = new Set();

    function add(element) {
      if (!element || seen.has(element)) return;
      seen.add(element);
      results.push(element);
    }

    function scan(root) {
      let matches = [];
      try {
        matches = [...root.querySelectorAll(selector)];
      } catch (error) {
        throw new Error(`Invalid selector: ${error.message}`);
      }
      matches.forEach(add);

      if (!includeShadow) return;
      let all = [];
      try {
        all = [...root.querySelectorAll('*')];
      } catch {
        all = [];
      }
      for (const element of all) {
        if (element.shadowRoot) scan(element.shadowRoot);
      }
    }

    scan(document);
    return results;
  }

  function matchesFilters(element, options) {
    if (options.tag && element.localName !== String(options.tag).toLowerCase()) return false;
    if (options.role && (element.getAttribute('role') || '').toLowerCase() !== String(options.role).toLowerCase()) return false;
    if (options.placeholder && !matchText(element.getAttribute('placeholder') || '', options.placeholder, options.exact)) return false;
    if (options.name && !matchText(element.getAttribute('name') || '', options.name, options.exact)) return false;
    if (options.href && !String(element.href || '').toLowerCase().includes(String(options.href).toLowerCase())) return false;
    if (options.text && !matchText(searchText(element), options.text, options.exact)) return false;
    return true;
  }

  function matchText(haystack, needle, exact) {
    const left = normalizeText(haystack);
    const right = normalizeText(needle);
    if (!right) return true;
    return exact ? left === right : left.includes(right);
  }

  function normalizeText(text) {
    return normalizeSpace(text).toLowerCase();
  }

  function normalizeSpace(text) {
    return String(text || '').replace(/\s+/g, ' ').trim();
  }

  function visible(element) {
    const style = window.getComputedStyle(element);
    const rect = element.getBoundingClientRect();
    return style.visibility !== 'hidden'
      && style.display !== 'none'
      && rect.width > 0
      && rect.height > 0;
  }

  function labelText(element) {
    if (element.labels?.length) {
      return [...element.labels].map((label) => label.innerText || label.textContent || '').join(' ');
    }

    const id = element.getAttribute('id');
    if (id) {
      const root = element.getRootNode?.() || document;
      const label = root.querySelector?.(`label[for="${CSS.escape(id)}"]`) || document.querySelector(`label[for="${CSS.escape(id)}"]`);
      if (label) return label.innerText || label.textContent || '';
    }

    const wrappingLabel = element.closest?.('label');
    return wrappingLabel ? (wrappingLabel.innerText || wrappingLabel.textContent || '') : '';
  }

  function textOf(element) {
    const pieces = [
      element.innerText,
      element.value,
      element.getAttribute('aria-label'),
      element.getAttribute('alt'),
      element.getAttribute('title'),
      element.getAttribute('placeholder'),
      labelText(element),
    ];
    return normalizeSpace(pieces.filter(Boolean).join(' ')).slice(0, 300);
  }

  function searchText(element) {
    return [
      textOf(element),
      element.getAttribute('name'),
      element.href,
    ].filter(Boolean).join(' ');
  }

  function describeElement(element) {
    const rect = element.getBoundingClientRect();
    return {
      ref: ensureRef(element),
      inShadowRoot: isInShadowRoot(element),
      tag: element.localName,
      selector: cssPath(element),
      text: textOf(element),
      type: element.getAttribute('type') || undefined,
      role: element.getAttribute('role') || undefined,
      name: element.getAttribute('name') || undefined,
      placeholder: element.getAttribute('placeholder') || undefined,
      href: element.href || undefined,
      disabled: Boolean(element.disabled || element.getAttribute('aria-disabled') === 'true'),
      rect: {
        x: Math.round(rect.x),
        y: Math.round(rect.y),
        width: Math.round(rect.width),
        height: Math.round(rect.height),
      },
    };
  }

  function ensureRef(element) {
    for (const [ref, stored] of refElements.entries()) {
      if (stored === element) return ref;
    }
    const ref = `r${Date.now().toString(36)}-${++refCounter}`;
    refElements.set(ref, element);
    return ref;
  }

  function isInShadowRoot(element) {
    const root = element.getRootNode?.();
    return Boolean(root && root !== document && root.host);
  }

  function cssPath(element) {
    if (element.id) return `#${CSS.escape(element.id)}`;
    const parts = [];
    for (let node = element; node && node.nodeType === Node.ELEMENT_NODE && node !== document.body; node = node.parentElement) {
      let part = node.localName;
      if (node.classList.length) {
        part += `.${[...node.classList].slice(0, 2).map((name) => CSS.escape(name)).join('.')}`;
      }
      const parent = node.parentElement;
      if (parent) {
        const siblings = [...parent.children].filter((child) => child.localName === node.localName);
        if (siblings.length > 1) part += `:nth-of-type(${siblings.indexOf(node) + 1})`;
      }
      parts.unshift(part);
    }
    return parts.join(' > ');
  }

  function elementFromTarget(target, options = {}) {
    if (target.ref) {
      const element = refElements.get(target.ref);
      if (!element) throw new Error(`No element found for ref: ${target.ref}`);
      if (options.typeable && !typeable(element)) throw new Error(`Element is not typeable: ${target.ref}`);
      return element;
    }

    if (target.selector) {
      const matches = queryAllDeep(target.selector, true);
      const element = matches[0];
      if (!element) throw new Error(`No element found for selector: ${target.selector}`);
      if (options.typeable && !typeable(element)) throw new Error(`Element is not typeable: ${target.selector}`);
      return element;
    }

    if (options.typeable) {
      const typeableElement = findTypeableElement(target);
      if (typeableElement) return typeableElement;
      throw new Error('No typeable element found for target');
    }

    const candidates = describeElements({
      text: target.text,
      exact: target.exact,
      role: target.role,
      limit: Math.max(1, Number(target.index || 0) + 1),
    });
    const match = candidates[Number(target.index || 0)];
    if (!match) throw new Error(`No clickable element found for text: ${target.text}`);
    return refElements.get(match.ref);
  }

  function typeable(element) {
    return element.isContentEditable
      || element.localName === 'textarea'
      || element.localName === 'select'
      || (element.localName === 'input' && element.type !== 'hidden');
  }

  function findTypeableElement(target) {
    const candidates = queryAllDeep('input, textarea, select, [contenteditable="true"]', true)
      .filter(visible)
      .filter(typeable);

    if (target.placeholder) {
      const match = candidates.find((element) => matchText(element.getAttribute('placeholder') || '', target.placeholder, target.exact));
      if (match) return match;
    }

    if (target.name) {
      const match = candidates.find((element) => matchText(element.getAttribute('name') || '', target.name, target.exact));
      if (match) return match;
    }

    if (target.text) {
      const match = candidates.find((element) => matchText(searchText(element), target.text, target.exact));
      if (match) return match;

      const labels = queryAllDeep('label', true).filter(visible);
      const label = labels.find((candidate) => matchText(candidate.innerText || candidate.textContent || '', target.text, target.exact));
      if (label) {
        if (label.control && typeable(label.control)) return label.control;
        const nested = queryAllWithin(label, 'input, textarea, select, [contenteditable="true"]')[0];
        if (nested && typeable(nested)) return nested;
      }
    }

    return null;
  }

  function queryAllWithin(root, selector) {
    try {
      return [...root.querySelectorAll(selector)];
    } catch {
      return [];
    }
  }

  function clickTarget(target) {
    const element = elementFromTarget(target);
    if (!element) throw new Error('Target resolved to no element');
    element.scrollIntoView({ block: 'center', inline: 'center' });
    element.focus?.();
    element.click();
    return { clicked: true, target: describeElement(element) };
  }

  function typeIntoTarget(target, text, replace, submit) {
    const element = elementFromTarget(target, { typeable: true });
    element.scrollIntoView({ block: 'center', inline: 'center' });
    element.focus();

    if (element.isContentEditable) {
      if (replace) element.textContent = '';
      document.execCommand('insertText', false, text);
      element.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text }));
    } else if (element.localName === 'select') {
      setSelectValue(element, text);
    } else if ('value' in element) {
      setNativeValue(element, replace ? text : `${element.value || ''}${text}`);
      element.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text }));
      element.dispatchEvent(new Event('change', { bubbles: true }));
    } else {
      throw new Error('Element is not typeable');
    }

    let submitted = false;
    if (submit) {
      const form = element.closest('form');
      if (form) {
        form.requestSubmit();
        submitted = true;
      } else {
        element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', bubbles: true }));
        element.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', bubbles: true }));
      }
    }

    return { typed: true, submitted, target: describeElement(element) };
  }

  function setNativeValue(element, value) {
    const prototype = Object.getPrototypeOf(element);
    const descriptor = Object.getOwnPropertyDescriptor(prototype, 'value');
    if (descriptor?.set) {
      descriptor.set.call(element, value);
    } else {
      element.value = value;
    }
  }

  function setSelectValue(element, text) {
    const option = [...element.options].find((candidate) => (
      candidate.value === text
      || normalizeText(candidate.textContent) === normalizeText(text)
    ));
    if (!option) throw new Error(`No select option found for: ${text}`);
    element.value = option.value;
    element.dispatchEvent(new Event('input', { bubbles: true }));
    element.dispatchEvent(new Event('change', { bubbles: true }));
  }

  function scrollPage(x, y) {
    window.scrollBy({ left: x, top: y, behavior: 'instant' });
    return { scroll: { x: scrollX, y: scrollY } };
  }

  function waitForSelectorInPage(selector, mustBeVisible, timeoutMs) {
    return waitFor(() => {
      const element = queryAllDeep(selector, true)[0];
      if (!element) return null;
      if (mustBeVisible && !visible(element)) return null;
      return { found: true, target: describeElement(element) };
    }, timeoutMs, `Timed out waiting for selector: ${selector}`);
  }

  function waitForTextInPage(text, exact, timeoutMs) {
    return waitFor(() => {
      const bodyText = document.body?.innerText || '';
      if (!matchText(bodyText, text, exact)) return null;
      return { found: true, text };
    }, timeoutMs, `Timed out waiting for text: ${text}`);
  }

  function waitUntilIdleInPage(idleMs, timeoutMs) {
    return new Promise((resolve, reject) => {
      const startedAt = Date.now();
      let lastMutationAt = Date.now();
      let timer = null;
      const observer = new MutationObserver(() => {
        lastMutationAt = Date.now();
      });

      const cleanup = () => {
        observer.disconnect();
        if (timer) clearInterval(timer);
      };

      observer.observe(document.documentElement, { childList: true, subtree: true, attributes: true, characterData: true });
      timer = setInterval(() => {
        const ready = document.readyState === 'interactive' || document.readyState === 'complete';
        const idle = Date.now() - lastMutationAt >= idleMs;
        if (ready && idle) {
          cleanup();
          resolve({ idle: true, readyState: document.readyState, waitedMs: Date.now() - startedAt });
        } else if (Date.now() - startedAt >= timeoutMs) {
          cleanup();
          reject(new Error(`Timed out waiting for page idle after ${timeoutMs}ms`));
        }
      }, 100);
    });
  }

  function waitFor(check, timeoutMs, timeoutMessage) {
    return new Promise((resolve, reject) => {
      const startedAt = Date.now();
      const timer = setInterval(() => {
        let value = null;
        try {
          value = check();
        } catch (error) {
          clearInterval(timer);
          reject(error);
          return;
        }

        if (value) {
          clearInterval(timer);
          resolve({ ...value, waitedMs: Date.now() - startedAt });
        } else if (Date.now() - startedAt >= timeoutMs) {
          clearInterval(timer);
          reject(new Error(timeoutMessage));
        }
      }, 100);
    });
  }

  globalThis.__agentChromeBridge = {
    version: BRIDGE_VERSION,
    bridgeInfo,
    historyBack,
    historyForward,
    snapshotPage,
    findElements,
    findTypeableElements,
    extractLinks,
    extractTables,
    readArticle,
    getNetworkEntries,
    clickTarget,
    typeIntoTarget,
    scrollPage,
    waitForSelectorInPage,
    waitForTextInPage,
    waitUntilIdleInPage,
  };
})();
