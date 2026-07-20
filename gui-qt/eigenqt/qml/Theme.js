.pragma library

// Eigen design tokens — graphite control surface with high-signal accents.
// Keep structure neutral; use color to distinguish intent, not to decorate.

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
