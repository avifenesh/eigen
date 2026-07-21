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

    stateHover: "#12dde4e3",
    stateActive: "#2e3e9e96",
    stateSelected: "#29d08c5e",

    borderHairline: "#21dde4e3",
    borderSubtle: "#36dde4e3",
    borderStrong: "#57dde4e3",
    borderBrand: "#b869c2b8",
    borderFocus: "#b8d08c5e",
    divider: "#21dde4e3",

    textPrimary: "#dde4e3",
    textSecondary: "#aab6b3",
    textMuted: "#8d9b98",
    textGhost: "#74817e",

    brand: "#3e9e96",
    brandStrong: "#347f79",
    brandBright: "#69c2b8",
    brandDim: "#2a6e68",
    brandForeground: "#0b0e0f",
    brandDimForeground: "#f8fbfa",

    focus: "#d08c5e",
    focusBright: "#e8a878",

    accent: "#9e7ba6",
    accentBg: "#269e7ba6",
    borderAccentFaint: "#669e7ba6",

    success: "#7ba86b",
    successBg: "#267ba86b",
    warn: "#c9a24b",
    info: "#6fb7e8",
    warnBg: "#26c9a24b",
    error: "#c06a5e",
    errorBg: "#29c06a5e",
    working: "#d08c5e",
    workingBg: "#29d08c5e",

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
    borderBrandFaint: "#6169c2b8",
    stateFocusBg: "#1fd08c5e",
    textFaint: "#5d6966",
    brandBg: "#2b3e9e96"
}

var paletteName = startupPaletteName()
if (paletteName === "studio") applyStudioPalette()
else if (paletteName === "nord") applyNordPalette()
else if (paletteName === "gruvbox") applyGruvboxPalette()

function startupPaletteName() {
    if (typeof Qt === "undefined" || !Qt.application || !Qt.application.arguments)
        return "deepteal"
    var args = Qt.application.arguments
    for (var i = 0; i < args.length; ++i) {
        var match = /^--eigen-qt-theme=(deepteal|studio|nord|gruvbox)$/.exec(args[i])
        if (match) return match[1]
    }
    return "deepteal"
}

function applyStudioPalette() {
    colors.bgWell = "#e7ebee"
    colors.bgBase = "#f4f6f8"
    colors.surfaceRaised = "#ffffff"
    colors.surfaceRaised2 = "#eef1f4"
    colors.surfaceOverlay = "#e0e5e9"
    colors.bgInset = "#f0f3f5"
    colors.stateHover = "#0f17212b"
    colors.stateActive = "#1c0b736a"
    colors.stateSelected = "#1fc55338"
    colors.stateFocusBg = "#17c55338"
    colors.borderHairline = "#2417212b"
    colors.borderSubtle = "#3817212b"
    colors.borderStrong = "#5917212b"
    colors.borderBrand = "#940b736a"
    colors.borderBrandFaint = "#4d0b736a"
    colors.borderFocus = "#a8c55338"
    colors.divider = "#2117212b"
    colors.textPrimary = "#17212b"
    colors.textSecondary = "#3d4c59"
    colors.textMuted = "#62717e"
    colors.textGhost = "#7f8a94"
    colors.textFaint = "#929ba3"
    colors.brand = "#0b736a"
    colors.brandStrong = "#075a54"
    colors.brandBright = "#117f76"
    colors.brandDim = "#07554f"
    colors.brandForeground = "#ffffff"
    colors.brandDimForeground = "#ffffff"
    colors.brandBg = "#1c0b736a"
    colors.focus = "#c55338"
    colors.focusBright = "#9c3825"
    colors.accent = "#66558f"
    colors.accentBg = "#1b66558f"
    colors.borderAccentFaint = "#5766558f"
    colors.success = "#2f7d4a"
    colors.successBg = "#1d2f7d4a"
    colors.warn = "#8a6418"
    colors.warnBg = "#1f8a6418"
    colors.info = "#236b9a"
    colors.error = "#b4443a"
    colors.errorBg = "#1cb4443a"
    colors.working = "#b94f32"
    colors.workingBg = "#1db94f32"
    colors.dotWorking = colors.working
    colors.dotLive = colors.brand
    colors.dotIdle = colors.textGhost
    colors.dotOk = colors.success
    colors.dotWarn = colors.warn
    colors.dotError = colors.error
    colors.diffAddBg = "#e3f2e8"
    colors.diffAddGutter = "#2f7d4a"
    colors.diffDelBg = "#f8e5e2"
    colors.diffDelGutter = "#b4443a"
    colors.synBg = "#f1f3f5"
    colors.synText = "#202b35"
    colors.synKeyword = "#694597"
    colors.synType = "#7b5a10"
    colors.synFunc = "#236b9a"
    colors.synString = "#2f7445"
    colors.synNumber = "#a3432b"
    colors.synComment = "#6d7882"
    colors.synPunct = "#4c5a66"
    colors.synBuiltin = "#086b63"
    colors.bgRaised = colors.surfaceRaised
    colors.bgRaised2 = colors.surfaceRaised2
    colors.bgOverlay = colors.surfaceOverlay
}

function applyNordPalette() {
    colors.bgWell = "#15191f"
    colors.bgBase = "#1b1f27"
    colors.surfaceRaised = "#222734"
    colors.surfaceRaised2 = "#2b3140"
    colors.surfaceOverlay = "#353c4d"
    colors.bgInset = "#171b22"
    colors.stateHover = "#0affffff"
    colors.stateActive = "#12ffffff"
    colors.stateSelected = "#24d1a0b0"
    colors.stateFocusBg = "#1ad1a0b0"
    colors.borderHairline = "#14d8dee9"
    colors.borderSubtle = "#1fd8dee9"
    colors.borderStrong = "#33d8dee9"
    colors.borderBrand = "#8c81a1c1"
    colors.borderBrandFaint = "#3d81a1c1"
    colors.borderFocus = "#9ed1a0b0"
    colors.divider = "#12d8dee9"
    colors.textPrimary = "#d8dee9"
    colors.textSecondary = "#9aa5b8"
    colors.textMuted = "#79839a"
    colors.textGhost = "#5b657a"
    colors.textFaint = "#4a5365"
    colors.brand = "#81a1c1"
    colors.brandStrong = "#5e81ac"
    colors.brandBright = "#b3c4d8"
    colors.brandDim = "#4c6a8a"
    colors.brandForeground = "#1b1f27"
    colors.brandDimForeground = "#f8fbfa"
    colors.brandBg = "#1f81a1c1"
    colors.focus = "#d1a0b0"
    colors.focusBright = "#edc3d0"
    colors.accent = "#b48ead"
    colors.accentBg = "#24b48ead"
    colors.borderAccentFaint = "#57b48ead"
    colors.success = "#a3be8c"
    colors.successBg = "#1fa3be8c"
    colors.warn = "#ebcb8b"
    colors.warnBg = "#1febcb8b"
    colors.info = "#88c0d0"
    colors.error = "#bf616a"
    colors.errorBg = "#21bf616a"
    colors.working = "#d08770"
    colors.workingBg = "#1fd08770"
    colors.dotWorking = colors.brand
    colors.dotLive = colors.brand
    colors.dotIdle = colors.textMuted
    colors.dotOk = colors.success
    colors.dotWarn = colors.warn
    colors.dotError = colors.error
    colors.diffAddBg = "#d91e2a22"
    colors.diffAddGutter = "#3a6b4c"
    colors.diffDelBg = "#d92e2026"
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
    colors.stateHover = "#0affffff"
    colors.stateActive = "#12ffffff"
    colors.stateSelected = "#26d3869b"
    colors.stateFocusBg = "#1ad3869b"
    colors.borderHairline = "#14ebdbb2"
    colors.borderSubtle = "#1febdbb2"
    colors.borderStrong = "#33ebdbb2"
    colors.borderBrand = "#8c83a598"
    colors.borderBrandFaint = "#3d83a598"
    colors.borderFocus = "#9ed3869b"
    colors.divider = "#12ebdbb2"
    colors.textPrimary = "#ebdbb2"
    colors.textSecondary = "#a89984"
    colors.textMuted = "#928374"
    colors.textGhost = "#7c6f64"
    colors.textFaint = "#504945"
    colors.brand = "#83a598"
    colors.brandStrong = "#689d6a"
    colors.brandBright = "#bdddd0"
    colors.brandDim = "#427b58"
    colors.brandForeground = "#282828"
    colors.brandDimForeground = "#f8fbfa"
    colors.brandBg = "#2183a598"
    colors.focus = "#d3869b"
    colors.focusBright = "#e9a9bd"
    colors.accent = "#b16286"
    colors.accentBg = "#29b16286"
    colors.borderAccentFaint = "#61b16286"
    colors.success = "#b8bb26"
    colors.successBg = "#1fb8bb26"
    colors.warn = "#fabd2f"
    colors.warnBg = "#1ffabd2f"
    colors.info = "#83a598"
    colors.error = "#fb4934"
    colors.errorBg = "#21fb4934"
    colors.working = "#fe8019"
    colors.workingBg = "#1ffe8019"
    colors.dotWorking = colors.brand
    colors.dotLive = colors.brand
    colors.dotIdle = colors.textMuted
    colors.dotOk = colors.success
    colors.dotWarn = colors.warn
    colors.dotError = colors.error
    colors.diffAddBg = "#d928280f"
    colors.diffAddGutter = "#4a6b1f"
    colors.diffDelBg = "#d9321a16"
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
