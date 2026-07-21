.pragma library

// Eigen design tokens. The default Qt palette follows the product's deep-teal
// roles: petrol-teal is structural brand, warm clay is focus/selection, and
// violet is tool metadata. The saved theme is resolved before QML loads.

var colors = {
    bgWell: "#101619",
    bgBase: "#0b0e0f",
    surfaceRaised: "#151d20",
    surfaceRaised2: "#1d282c",
    surfaceOverlay: "#243136",
    bgInset: "#080b0c",

    stateHover: "rgba(221,228,227,0.07)",
    stateActive: "rgba(62,158,150,0.18)",
    stateSelected: "rgba(208,140,94,0.16)",

    borderHairline: "rgba(221,228,227,0.13)",
    borderSubtle: "rgba(221,228,227,0.21)",
    borderStrong: "rgba(221,228,227,0.34)",
    borderBrand: "rgba(105,194,184,0.72)",
    borderFocus: "rgba(208,140,94,0.72)",
    divider: "rgba(221,228,227,0.13)",

    textPrimary: "#dde4e3",
    textSecondary: "#aab6b3",
    textMuted: "#8d9b98",
    textGhost: "#74817e",

    brand: "#3e9e96",
    brandStrong: "#347f79",
    brandBright: "#69c2b8",
    brandDim: "#2a6e68",

    focus: "#d08c5e",
    focusBright: "#e8a878",

    accent: "#9e7ba6",
    accentBg: "rgba(158,123,166,0.15)",
    borderAccentFaint: "rgba(158,123,166,0.4)",

    success: "#7ba86b",
    successBg: "rgba(123,168,107,0.15)",
    warn: "#c9a24b",
    info: "#6fb7e8",
    warnBg: "rgba(201,162,75,0.15)",
    error: "#c06a5e",
    errorBg: "rgba(192,106,94,0.16)",
    working: "#d08c5e",
    workingBg: "rgba(208,140,94,0.16)",

    // Status dots
    dotWorking: "#d08c5e",
    dotLive: "#3e9e96",
    dotIdle: "#74817e",
    dotOk: "#7ba86b",
    dotWarn: "#c9a24b",
    dotError: "#c06a5e",

    // Diff colors (from tokens.css --diff-*)
    diffAddBg: "#10261c",
    diffAddGutter: "#7ba86b",
    diffDelBg: "#2a1517",
    diffDelGutter: "#c06a5e",

    // Syntax highlighting (code surfaces)
    synBg: "#11171a",
    synText: "#dde4e3",
    synKeyword: "#c58fd8",
    synType: "#e0b36a",
    synFunc: "#6fb7e8",
    synString: "#8fc98a",
    synNumber: "#e8a878",
    synComment: "#71807c",
    synPunct: "#9ab0ac",
    synBuiltin: "#69c2b8",

    // Additional tokens
    bgRaised: "#151d20",
    bgRaised2: "#1d282c",
    bgOverlay: "#243136",
    borderBrandFaint: "rgba(105,194,184,0.38)",
    stateFocusBg: "rgba(208,140,94,0.12)",
    textFaint: "#5d6966",
    brandBg: "rgba(62,158,150,0.17)"
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
    colors.stateSelected = "rgba(209,160,176,0.14)"
    colors.stateFocusBg = "rgba(209,160,176,0.1)"
    colors.borderHairline = "rgba(216,222,233,0.08)"
    colors.borderSubtle = "rgba(216,222,233,0.12)"
    colors.borderStrong = "rgba(216,222,233,0.2)"
    colors.borderBrand = "rgba(129,161,193,0.55)"
    colors.borderBrandFaint = "rgba(129,161,193,0.24)"
    colors.borderFocus = "rgba(209,160,176,0.62)"
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
    colors.focus = "#d1a0b0"
    colors.focusBright = "#edc3d0"
    colors.accent = "#b48ead"
    colors.accentBg = "rgba(180,142,173,0.14)"
    colors.borderAccentFaint = "rgba(180,142,173,0.34)"
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
    colors.stateSelected = "rgba(211,134,155,0.15)"
    colors.stateFocusBg = "rgba(211,134,155,0.1)"
    colors.borderHairline = "rgba(235,219,178,0.08)"
    colors.borderSubtle = "rgba(235,219,178,0.12)"
    colors.borderStrong = "rgba(235,219,178,0.2)"
    colors.borderBrand = "rgba(131,165,152,0.55)"
    colors.borderBrandFaint = "rgba(131,165,152,0.24)"
    colors.borderFocus = "rgba(211,134,155,0.62)"
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
    colors.focus = "#d3869b"
    colors.focusBright = "#e9a9bd"
    colors.accent = "#b16286"
    colors.accentBg = "rgba(177,98,134,0.16)"
    colors.borderAccentFaint = "rgba(177,98,134,0.38)"
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
    display: 30,
    h1: 24,
    h2: 19,
    h3: 15,
    body: 14,     // fsBody
    bodySm: 13,   // fsBodySm
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
