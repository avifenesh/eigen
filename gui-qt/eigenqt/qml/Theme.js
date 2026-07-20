.pragma library

// Eigen design tokens — restrained graphite structure with high-signal accents.
// The saved theme is passed by main.py before QML loads, so it applies at app
// startup just like the TUI and legacy GUI. Geometry stays shared across themes.

var colors = {
    bgWell: "#0d1014",
    bgBase: "#15191e",
    surfaceRaised: "#1d232a",
    surfaceRaised2: "#252d35",
    surfaceOverlay: "#303a44",
    bgInset: "#11161b",

    stateHover: "rgba(255,255,255,0.075)",
    stateActive: "rgba(91,214,194,0.18)",
    stateSelected: "rgba(91,214,194,0.16)",

    borderHairline: "rgba(233,240,238,0.11)",
    borderSubtle: "rgba(233,240,238,0.18)",
    borderStrong: "rgba(233,240,238,0.3)",
    borderBrand: "rgba(91,214,194,0.68)",
    divider: "rgba(233,240,238,0.12)",

    textPrimary: "#f1f5f4",
    textSecondary: "#c0cac7",
    textMuted: "#8d9996",
    textGhost: "#65716f",

    brand: "#5bd6c2",
    brandStrong: "#2aa892",
    brandBright: "#a7f2e6",
    brandDim: "#236f63",

    accent: "#8bb9ff",
    accentBg: "rgba(139,185,255,0.14)",
    borderAccentFaint: "rgba(139,185,255,0.32)",

    success: "#a6da7a",
    successBg: "rgba(166,218,122,0.14)",
    warn: "#f2b867",
    info: "#8bb9ff",
    warnBg: "rgba(242,184,103,0.14)",
    error: "#ff9382",
    errorBg: "rgba(255,147,130,0.14)",
    working: "#e9a978",
    workingBg: "rgba(233,169,120,0.14)",

    // Status dots
    dotWorking: "#5bd6c2",  // brand mint, breathes
    dotLive: "#5bd6c2",     // static mint
    dotIdle: "#8d9996",     // textMuted
    dotOk: "#a6da7a",       // success
    dotWarn: "#f2b867",     // warn
    dotError: "#ff9382",    // error

    // Diff colors (from tokens.css --diff-*)
    diffAddBg: "rgba(33,61,43,0.88)",
    diffAddGutter: "#75be79",
    diffDelBg: "rgba(82,37,40,0.88)",
    diffDelGutter: "#e58074",

    // Syntax highlighting (code surfaces)
    synBg: "#11161b",
    synText: "#d5dfdc",
    synKeyword: "#d6a2ed",
    synType: "#f2b867",
    synFunc: "#8bb9ff",
    synString: "#a6da7a",
    synNumber: "#efa979",
    synComment: "#71807c",
    synPunct: "#acbbb7",
    synBuiltin: "#5bd6c2",

    // Additional tokens
    bgRaised: "#1d232a",
    bgRaised2: "#252d35",
    bgOverlay: "#303a44",
    borderBrandFaint: "rgba(91,214,194,0.34)",
    stateFocusBg: "rgba(91,214,194,0.12)",
    textFaint: "#56615f",
    brandBg: "rgba(91,214,194,0.16)"
}

var paletteName = startupPaletteName()
if (paletteName === "nord") applyNordPalette()
else if (paletteName === "gruvbox") applyGruvboxPalette()

function startupPaletteName() {
    if (typeof Qt === "undefined" || !Qt.application || !Qt.application.arguments)
        return "deepteal"
    var args = Qt.application.arguments
    for (var i = 0; i < args.length; ++i) {
        var match = /^--eigen-qt-theme=(deepteal|nord|gruvbox)$/.exec(args[i])
        if (match) return match[1]
    }
    return "deepteal"
}

function applyNordPalette() {
    colors.bgWell = "#15191f"
    colors.bgBase = "#1b1f27"
    colors.surfaceRaised = "#222734"
    colors.surfaceRaised2 = "#2b3140"
    colors.surfaceOverlay = "#353c4d"
    colors.bgInset = "#171b22"
    colors.stateHover = "rgba(255,255,255,0.04)"
    colors.stateActive = "rgba(255,255,255,0.07)"
    colors.stateSelected = "rgba(129,161,193,0.12)"
    colors.stateFocusBg = "rgba(129,161,193,0.08)"
    colors.borderHairline = "rgba(216,222,233,0.08)"
    colors.borderSubtle = "rgba(216,222,233,0.12)"
    colors.borderStrong = "rgba(216,222,233,0.2)"
    colors.borderBrand = "rgba(129,161,193,0.55)"
    colors.borderBrandFaint = "rgba(129,161,193,0.24)"
    colors.divider = "rgba(216,222,233,0.07)"
    colors.textPrimary = "#d8dee9"
    colors.textSecondary = "#9aa5b8"
    colors.textMuted = "#79839a"
    colors.textGhost = "#5b657a"
    colors.textFaint = "#4a5365"
    colors.brand = "#81a1c1"
    colors.brandStrong = "#5e81ac"
    colors.brandBright = "#b3c4d8"
    colors.brandDim = "#4c6a8a"
    colors.brandBg = "rgba(129,161,193,0.12)"
    colors.accent = "#88c0d0"
    colors.accentBg = "rgba(136,192,208,0.12)"
    colors.borderAccentFaint = "rgba(136,192,208,0.28)"
    colors.success = "#a3be8c"
    colors.successBg = "rgba(163,190,140,0.12)"
    colors.warn = "#ebcb8b"
    colors.warnBg = "rgba(235,203,139,0.12)"
    colors.info = "#88c0d0"
    colors.error = "#bf616a"
    colors.errorBg = "rgba(191,97,106,0.13)"
    colors.working = "#d08770"
    colors.workingBg = "rgba(208,135,112,0.12)"
    colors.dotWorking = colors.brand
    colors.dotLive = colors.brand
    colors.dotIdle = colors.textMuted
    colors.dotOk = colors.success
    colors.dotWarn = colors.warn
    colors.dotError = colors.error
    colors.diffAddBg = "rgba(30,42,34,0.85)"
    colors.diffAddGutter = "#3a6b4c"
    colors.diffDelBg = "rgba(46,32,38,0.85)"
    colors.diffDelGutter = "#7a4640"
    colors.synBg = "#171b22"
    colors.synText = "#d8dee9"
    colors.synKeyword = "#b48ead"
    colors.synType = "#ebcb8b"
    colors.synFunc = "#88c0d0"
    colors.synString = "#a3be8c"
    colors.synNumber = "#d08770"
    colors.synComment = "#616e88"
    colors.synPunct = "#9aa5b8"
    colors.synBuiltin = "#8fbcbb"
    colors.bgRaised = colors.surfaceRaised
    colors.bgRaised2 = colors.surfaceRaised2
    colors.bgOverlay = colors.surfaceOverlay
}

function applyGruvboxPalette() {
    colors.bgWell = "#1d2021"
    colors.bgBase = "#282828"
    colors.surfaceRaised = "#32302f"
    colors.surfaceRaised2 = "#3c3836"
    colors.surfaceOverlay = "#504945"
    colors.bgInset = "#1d2021"
    colors.stateHover = "rgba(255,255,255,0.04)"
    colors.stateActive = "rgba(255,255,255,0.07)"
    colors.stateSelected = "rgba(131,165,152,0.13)"
    colors.stateFocusBg = "rgba(131,165,152,0.08)"
    colors.borderHairline = "rgba(235,219,178,0.08)"
    colors.borderSubtle = "rgba(235,219,178,0.12)"
    colors.borderStrong = "rgba(235,219,178,0.2)"
    colors.borderBrand = "rgba(131,165,152,0.55)"
    colors.borderBrandFaint = "rgba(131,165,152,0.24)"
    colors.divider = "rgba(235,219,178,0.07)"
    colors.textPrimary = "#ebdbb2"
    colors.textSecondary = "#a89984"
    colors.textMuted = "#928374"
    colors.textGhost = "#7c6f64"
    colors.textFaint = "#504945"
    colors.brand = "#83a598"
    colors.brandStrong = "#689d6a"
    colors.brandBright = "#bdddd0"
    colors.brandDim = "#427b58"
    colors.brandBg = "rgba(131,165,152,0.13)"
    colors.accent = "#8ec07c"
    colors.accentBg = "rgba(142,192,124,0.12)"
    colors.borderAccentFaint = "rgba(142,192,124,0.28)"
    colors.success = "#b8bb26"
    colors.successBg = "rgba(184,187,38,0.12)"
    colors.warn = "#fabd2f"
    colors.warnBg = "rgba(250,189,47,0.12)"
    colors.info = "#83a598"
    colors.error = "#fb4934"
    colors.errorBg = "rgba(251,73,52,0.13)"
    colors.working = "#fe8019"
    colors.workingBg = "rgba(254,128,25,0.12)"
    colors.dotWorking = colors.brand
    colors.dotLive = colors.brand
    colors.dotIdle = colors.textMuted
    colors.dotOk = colors.success
    colors.dotWarn = colors.warn
    colors.dotError = colors.error
    colors.diffAddBg = "rgba(40,40,15,0.85)"
    colors.diffAddGutter = "#4a6b1f"
    colors.diffDelBg = "rgba(50,26,22,0.85)"
    colors.diffDelGutter = "#8a3a30"
    colors.synBg = "#1d2021"
    colors.synText = "#ebdbb2"
    colors.synKeyword = "#fb4934"
    colors.synType = "#fabd2f"
    colors.synFunc = "#8ec07c"
    colors.synString = "#b8bb26"
    colors.synNumber = "#d3869b"
    colors.synComment = "#928374"
    colors.synPunct = "#a89984"
    colors.synBuiltin = "#8ec07c"
    colors.bgRaised = colors.surfaceRaised
    colors.bgRaised2 = colors.surfaceRaised2
    colors.bgOverlay = colors.surfaceOverlay
}

var uiFonts = ["Inter", "Noto Sans", "sans-serif"]
var monoFonts = ["JetBrains Mono", "JetBrainsMono Nerd Font", "DejaVu Sans Mono", "monospace"]

// 4px base spacing scale (matching tokens.css --sp-* naming)
var space = {
    xxs: 2,
    xs: 4,   // sp3
    sm: 6,   // sp4
    md: 8,   // sp5
    lg: 12,
    xl: 16,
    xxl: 20,
    xxxl: 24,
    xxxxl: 32
}

// Named spacing (matching QML references)
var sp3 = 4
var sp4 = 6
var sp5 = 8
var sp6 = 12

var radius = {
    xs: 3,
    sm: 5,   // rSm
    md: 6,
    lg: 8,
    xl: 16,
    full: 9999
}

var rSm = 5

var fontSize = {
    display: 28,
    h1: 22,
    h2: 18,
    h3: 15,
    body: 13,     // fsBody
    bodySm: 12,   // fsBodySm
    label: 12,    // fsLabel
    micro: 11,
    code: 13,     // fsCode
    codeSm: 12    // fsCodeSm
}

// Named font sizes (matching QML references)
var fsBodySm = 12
var fsLabel = 12
var fsCodeSm = 12

var fontWeight = {
    regular: 400,   // fwRegular
    medium: 500,
    semibold: 600,  // fwSemibold
    bold: 700
}

// Named font weights
var fwRegular = 400
var fwSemibold = 600

var duration = {
    instant: 80,
    fast: 140,
    base: 200,
    slow: 280,
    breath: 2600
}

// Continuous opacity/rotation animations keep Qt's render loop hot under
// software compositors. Keep state static by default; transient Behaviors above
// still provide lightweight interaction feedback.
var continuousMotion = false

function statusColor(status) {
    if (status === "working") return colors.dotWorking
    if (status === "error") return colors.dotError
    if (status === "success" || status === "done") return colors.dotOk
    if (status === "warn") return colors.dotWarn
    if (status === "live" || status === "active") return colors.dotLive
    return colors.dotIdle  // idle
}
