'use strict';

const TAB_GUARD = {
  expectedHost: { type: 'string', description: 'Optional safety guard. Refuse if the target tab host differs.' },
  expectedUrlIncludes: { type: 'string', description: 'Optional safety guard. Refuse if the target tab URL does not contain this text.' },
  lockId: { type: 'string', description: 'Optional tab lock id returned by chrome_lock_tab.' },
};

const OPTIONAL_TAB = {
  tabId: { type: 'number', description: 'Optional Chrome tab id. Defaults to active tab.' },
};

const FRAME_OPTIONS = {
  frameId: { type: 'number', description: 'Optional Chrome frame id. Defaults to all frames for reads and main frame for direct selector actions.' },
  includeFrames: { type: 'boolean', description: 'Search/read all frames. Defaults to true for read/find tools.' },
  includeShadow: { type: 'boolean', description: 'Search open shadow roots. Defaults to true.' },
};

const TARGET = {
  selector: { type: 'string', description: 'CSS selector to target.' },
  elementId: { type: 'string', description: 'Element id from the latest chrome_snapshot or chrome_find result for that tab.' },
};

const TOOLS = [
  {
    name: 'chrome_health',
    description: 'Return bridge diagnostics: socket path, extension connectivity, component versions, active locks, recent sanitized requests, and recent errors.',
    inputSchema: {
      type: 'object',
      properties: {
        tabId: { type: 'number', description: 'Optional tab id for the content-script version probe. Defaults to the active injectable tab.' },
      },
    },
  },
  {
    name: 'chrome_action_log',
    description: 'Return recent sanitized bridge requests and errors.',
    inputSchema: {
      type: 'object',
      properties: {
        limit: { type: 'number', description: 'Maximum entries to return. Defaults to 20.' },
      },
    },
  },
  {
    name: 'chrome_lock_tab',
    description: 'Lock a tab for one agent/client to reduce accidental cross-agent edits. Mutating calls to that tab require the returned lockId until it expires or is released.',
    inputSchema: {
      type: 'object',
      required: ['tabId'],
      properties: {
        tabId: { type: 'number', description: 'Chrome tab id to lock.' },
        owner: { type: 'string', description: 'Human-readable lock owner, for example eigen or another local client.' },
        ttlMs: { type: 'number', description: 'Lock lifetime in milliseconds. Defaults to 120000.' },
        note: { type: 'string', description: 'Optional reason for the lock.' },
      },
    },
  },
  {
    name: 'chrome_unlock_tab',
    description: 'Release a tab lock by lock id.',
    inputSchema: {
      type: 'object',
      required: ['lockId'],
      properties: {
        lockId: { type: 'string' },
      },
    },
  },
  {
    name: 'chrome_locks',
    description: 'List active tab locks.',
    inputSchema: { type: 'object', properties: {} },
  },
  {
    name: 'chrome_tabs',
    description: 'List open Chrome tabs in the user profile where the bridge extension is installed.',
    inputSchema: { type: 'object', properties: {} },
  },
  {
    name: 'chrome_active_tab',
    description: 'Return the active tab in the last focused Chrome window.',
    inputSchema: { type: 'object', properties: {} },
  },
  {
    name: 'chrome_select_tab',
    description: 'Focus a Chrome tab and its window.',
    inputSchema: {
      type: 'object',
      required: ['tabId'],
      properties: {
        tabId: { type: 'number', description: 'Chrome tab id to activate.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_new_tab',
    description: 'Create a new Chrome tab, optionally with a URL.',
    inputSchema: {
      type: 'object',
      properties: {
        url: { type: 'string', description: 'URL to open. Defaults to chrome://newtab.' },
        active: { type: 'boolean', description: 'Whether to make the tab active. Defaults to true.' },
      },
    },
  },
  {
    name: 'chrome_close_tab',
    description: 'Close a Chrome tab. Requires an explicit tab id to avoid accidental active-tab closes.',
    inputSchema: {
      type: 'object',
      required: ['tabId'],
      properties: {
        tabId: { type: 'number', description: 'Chrome tab id to close.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_reload',
    description: 'Reload a Chrome tab.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        bypassCache: { type: 'boolean', description: 'Bypass browser cache. Defaults to false.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_back',
    description: 'Navigate the tab one history entry back.',
    inputSchema: { type: 'object', properties: { ...OPTIONAL_TAB, ...FRAME_OPTIONS, ...TAB_GUARD } },
  },
  {
    name: 'chrome_forward',
    description: 'Navigate the tab one history entry forward.',
    inputSchema: { type: 'object', properties: { ...OPTIONAL_TAB, ...FRAME_OPTIONS, ...TAB_GUARD } },
  },
  {
    name: 'chrome_snapshot',
    description: 'Read a text and element snapshot from a Chrome tab. By default this aggregates all frames and open shadow roots. Elements include ids usable by chrome_click and chrome_type.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        maxText: { type: 'number', description: 'Maximum body text characters per frame. Defaults to 12000.' },
        maxElements: { type: 'number', description: 'Maximum actionable elements per frame. Defaults to 120.' },
      },
    },
  },
  {
    name: 'chrome_find',
    description: 'Find visible actionable elements by selector, text, role, tag, placeholder, name, or href across frames and open shadow roots.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        selector: { type: 'string', description: 'Optional CSS selector to filter from.' },
        text: { type: 'string', description: 'Visible text, aria label, title, alt text, placeholder, name, or href text to match.' },
        exact: { type: 'boolean', description: 'Require exact case-insensitive text match. Defaults to false.' },
        role: { type: 'string', description: 'ARIA role to match.' },
        tag: { type: 'string', description: 'Tag name to match, for example button, a, input.' },
        placeholder: { type: 'string', description: 'Placeholder text to match.' },
        name: { type: 'string', description: 'Input name attribute to match.' },
        href: { type: 'string', description: 'Href substring to match.' },
        limit: { type: 'number', description: 'Maximum elements per frame to return. Defaults to 20.' },
      },
    },
  },
  {
    name: 'chrome_extract_links',
    description: 'Extract links from a page across frames and open shadow roots.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        text: { type: 'string', description: 'Optional visible text filter.' },
        href: { type: 'string', description: 'Optional href substring filter.' },
        exact: { type: 'boolean', description: 'Require exact case-insensitive text match. Defaults to false.' },
        visible: { type: 'boolean', description: 'Only visible links. Defaults to true.' },
        limit: { type: 'number', description: 'Maximum links per frame. Defaults to 200.' },
      },
    },
  },
  {
    name: 'chrome_extract_tables',
    description: 'Extract visible HTML tables into rows.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        visible: { type: 'boolean', description: 'Only visible tables. Defaults to true.' },
        limit: { type: 'number', description: 'Maximum tables per frame. Defaults to 20.' },
        maxRows: { type: 'number', description: 'Maximum rows per table. Defaults to 50.' },
      },
    },
  },
  {
    name: 'chrome_read_article',
    description: 'Extract the most article-like main text from the page.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        maxText: { type: 'number', description: 'Maximum article text characters. Defaults to 30000.' },
      },
    },
  },
  {
    name: 'chrome_get_network',
    description: 'Read Resource Timing entries from the page, including frame metadata when available.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        limit: { type: 'number', description: 'Maximum resource entries per frame. Defaults to 200.' },
      },
    },
  },
  {
    name: 'chrome_navigate',
    description: 'Navigate a Chrome tab to a URL.',
    inputSchema: {
      type: 'object',
      required: ['url'],
      properties: {
        ...OPTIONAL_TAB,
        url: { type: 'string' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_click',
    description: 'Click an element by CSS selector, snapshot element id, or visible text match. Element ids support frame and open-shadow-root targets.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        ...TARGET,
        text: { type: 'string', description: 'Visible text/label to click when no selector or elementId is supplied.' },
        exact: { type: 'boolean', description: 'Require exact case-insensitive text match for text targeting. Defaults to false.' },
        role: { type: 'string', description: 'ARIA role to prefer for text targeting.' },
        index: { type: 'number', description: 'Zero-based match index for text targeting. Defaults to 0.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_type',
    description: 'Type into an input, textarea, select, or contenteditable element by selector, element id, label, placeholder, or name.',
    inputSchema: {
      type: 'object',
      required: ['text'],
      properties: {
        ...OPTIONAL_TAB,
        ...FRAME_OPTIONS,
        ...TARGET,
        targetText: { type: 'string', description: 'Visible label/text near the typeable element.' },
        placeholder: { type: 'string', description: 'Placeholder text to target.' },
        name: { type: 'string', description: 'Input name attribute to target.' },
        text: { type: 'string', description: 'Text to type.' },
        replace: { type: 'boolean', description: 'Replace current value instead of appending. Defaults to true.' },
        submit: { type: 'boolean', description: 'Submit the nearest form after typing. Defaults to false.' },
        index: { type: 'number', description: 'Zero-based match index for text targeting. Defaults to 0.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_scroll',
    description: 'Scroll a Chrome tab or frame by pixels.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        frameId: FRAME_OPTIONS.frameId,
        x: { type: 'number', description: 'Horizontal pixels. Defaults to 0.' },
        y: { type: 'number', description: 'Vertical pixels. Defaults to 700.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_wait_for_selector',
    description: 'Wait until a selector exists, and optionally until it is visible.',
    inputSchema: {
      type: 'object',
      required: ['selector'],
      properties: {
        ...OPTIONAL_TAB,
        frameId: FRAME_OPTIONS.frameId,
        selector: { type: 'string' },
        visible: { type: 'boolean', description: 'Require the element to be visible. Defaults to true.' },
        timeoutMs: { type: 'number', description: 'Timeout in milliseconds. Defaults to 10000.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_wait_for_text',
    description: 'Wait until page or frame text contains a string.',
    inputSchema: {
      type: 'object',
      required: ['text'],
      properties: {
        ...OPTIONAL_TAB,
        frameId: FRAME_OPTIONS.frameId,
        text: { type: 'string' },
        exact: { type: 'boolean', description: 'Require exact case-insensitive body text match. Defaults to false.' },
        timeoutMs: { type: 'number', description: 'Timeout in milliseconds. Defaults to 10000.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_wait_until_idle',
    description: 'Wait for document readiness and a short period without DOM mutations.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        frameId: FRAME_OPTIONS.frameId,
        idleMs: { type: 'number', description: 'Milliseconds with no DOM mutations before resolving. Defaults to 500.' },
        timeoutMs: { type: 'number', description: 'Timeout in milliseconds. Defaults to 10000.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_screenshot',
    description: 'Capture a visible-tab screenshot as a PNG data URL.',
    inputSchema: { type: 'object', properties: { ...OPTIONAL_TAB } },
  },
  {
    name: 'chrome_cdp_health',
    description: 'Check whether optional Chrome debugger/CDP tools are available.',
    inputSchema: { type: 'object', properties: {} },
  },
  {
    name: 'chrome_cdp_click',
    description: 'Use CDP Input.dispatchMouseEvent for a coordinate click. This may show Chrome debugger UI while attached.',
    inputSchema: {
      type: 'object',
      required: ['x', 'y'],
      properties: {
        ...OPTIONAL_TAB,
        x: { type: 'number' },
        y: { type: 'number' },
        button: { type: 'string', description: 'Mouse button. Defaults to left.' },
        clickCount: { type: 'number', description: 'Click count. Defaults to 1.' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_cdp_key',
    description: 'Use CDP to press a key in the target tab. Optional modifiers (Alt=1, Ctrl=2, Meta/Cmd=4, Shift=8 bitmask) or modifierKeys (friendly array like ["ctrl","shift"]) let you send Ctrl+K, Cmd+K, Shift+Tab, etc. This may show Chrome debugger UI while attached.',
    inputSchema: {
      type: 'object',
      required: ['key'],
      properties: {
        ...OPTIONAL_TAB,
        key: { type: 'string', description: 'Key value, for example Enter, Escape, Tab.' },
        code: { type: 'string', description: 'Optional physical key code.' },
        modifiers: {
          type: 'number',
          description: 'Optional CDP modifier bitmask (Alt=1, Ctrl=2, Meta/Cmd=4, Shift=8). OR-combined with modifierKeys when both supplied.',
        },
        modifierKeys: {
          type: 'array',
          items: { type: 'string' },
          description: 'Optional friendly modifier list, e.g. ["ctrl"], ["meta","shift"]. Accepts alt, ctrl/control, meta/cmd/command/super/win, shift. OR-combined with modifiers when both supplied.',
        },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_cdp_type',
    description: 'Use CDP Input.insertText to type into the currently focused element in the target tab. This may show Chrome debugger UI while attached.',
    inputSchema: {
      type: 'object',
      required: ['text'],
      properties: {
        ...OPTIONAL_TAB,
        text: { type: 'string' },
        ...TAB_GUARD,
      },
    },
  },
  {
    name: 'chrome_get_console',
    description: 'Attach CDP briefly and collect console/log/exception events during a wait window. This captures new events while attached, not all historical logs.',
    inputSchema: {
      type: 'object',
      properties: {
        ...OPTIONAL_TAB,
        waitMs: { type: 'number', description: 'How long to listen for console events. Defaults to 1000, max 10000.' },
        ...TAB_GUARD,
      },
    },
  },
];

const METHOD_BY_TOOL = {
  chrome_health: 'bridge.health',
  chrome_action_log: 'bridge.log',
  chrome_lock_tab: 'locks.acquire',
  chrome_unlock_tab: 'locks.release',
  chrome_locks: 'locks.list',
  chrome_tabs: 'tabs.list',
  chrome_active_tab: 'tabs.active',
  chrome_select_tab: 'tabs.select',
  chrome_new_tab: 'tabs.create',
  chrome_close_tab: 'tabs.close',
  chrome_reload: 'tabs.reload',
  chrome_back: 'tabs.back',
  chrome_forward: 'tabs.forward',
  chrome_snapshot: 'page.snapshot',
  chrome_find: 'page.find',
  chrome_extract_links: 'page.extractLinks',
  chrome_extract_tables: 'page.extractTables',
  chrome_read_article: 'page.readArticle',
  chrome_get_network: 'page.network',
  chrome_navigate: 'page.navigate',
  chrome_click: 'page.click',
  chrome_type: 'page.type',
  chrome_scroll: 'page.scroll',
  chrome_wait_for_selector: 'page.waitForSelector',
  chrome_wait_for_text: 'page.waitForText',
  chrome_wait_until_idle: 'page.waitUntilIdle',
  chrome_screenshot: 'page.screenshot',
  chrome_cdp_health: 'cdp.health',
  chrome_cdp_click: 'cdp.click',
  chrome_cdp_key: 'cdp.key',
  chrome_cdp_type: 'cdp.type',
  chrome_get_console: 'cdp.console',
};

const MUTATING_TOOLS = new Set([
  'chrome_lock_tab',
  'chrome_unlock_tab',
  'chrome_select_tab',
  'chrome_new_tab',
  'chrome_close_tab',
  'chrome_reload',
  'chrome_back',
  'chrome_forward',
  'chrome_navigate',
  'chrome_click',
  'chrome_type',
  'chrome_scroll',
  'chrome_cdp_click',
  'chrome_cdp_key',
  'chrome_cdp_type',
]);

const DESTRUCTIVE_TOOLS = new Set([
  'chrome_close_tab',
  'chrome_navigate',
  'chrome_click',
  'chrome_type',
  'chrome_cdp_click',
  'chrome_cdp_key',
  'chrome_cdp_type',
]);

module.exports = { TOOLS, METHOD_BY_TOOL, MUTATING_TOOLS, DESTRUCTIVE_TOOLS };
