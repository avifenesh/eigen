.pragma library

// Eigen design tokens — calm instrument panel palette (QML port).
// Exact match of internal/gui/frontend/src/styles/tokens.css deepteal theme.

var colors = {
    bgWell: "#07090a",
    bgBase: "#0b0e0f",
    surfaceRaised: "#11171a",
    surfaceRaised2: "#141b1e",
    surfaceOverlay: "#1a2428",
    bgInset: "rgba(0,0,0,0.22)",

    stateHover: "rgba(255,255,255,0.04)",
    stateActive: "rgba(255,255,255,0.07)",
    stateSelected: "rgba(105,194,184,0.1)",

    borderHairline: "rgba(221,228,227,0.07)",
    borderSubtle: "rgba(221,228,227,0.11)",
    borderStrong: "rgba(221,228,227,0.18)",
    borderBrand: "rgba(105,194,184,0.55)",
    divider: "rgba(221,228,227,0.06)",

    textPrimary: "#dde4e3",
    textSecondary: "#9aaaa7",
    textMuted: "#7e8e8b",
    textGhost: "#52605e",

    brand: "#69c2b8",
    brandStrong: "#3e9e96",
    brandBright: "#8ad6cc",
    brandDim: "#2e7670",

    accent: "#5fb0c4",

    success: "#8fc98a",
    successBg: "rgba(143,201,138,0.12)",
    warn: "#e0b36a",
    info: "#5fb0c4",
    warnBg: "rgba(224,179,106,0.12)",
    error: "#d67e72",
    errorBg: "rgba(214,126,114,0.13)",
    working: "#d08c5e",
    workingBg: "rgba(208,140,94,0.12)",

    // Status dots
    dotWorking: "#69c2b8",  // brand teal, breathes
    dotLive: "#69c2b8",     // static teal
    dotIdle: "#7e8e8b",     // textMuted
    dotOk: "#8fc98a",       // success
    dotWarn: "#e0b36a",     // warn
    dotError: "#d67e72",    // error

    // Diff colors (from tokens.css --diff-*)
    diffAddBg: "rgba(16,38,28,0.85)",
    diffAddGutter: "#3a6b4c",
    diffDelBg: "rgba(42,21,23,0.85)",
    diffDelGutter: "#7a4640",

    // Syntax highlighting (code surfaces)
    synBg: "#0a1012",
    synText: "#c7d2d0",
    synKeyword: "#c58fd8",
    synType: "#e0b36a",
    synFunc: "#6fb7e8",
    synString: "#8fc98a",
    synNumber: "#e8a878",
    synComment: "#5e6e6a",
    synPunct: "#9ab0ac",
    synBuiltin: "#69c2b8",

    // Additional tokens
    bgRaised: "#11171a",
    bgRaised2: "#141b1e",
    bgOverlay: "#1a2428",
    borderBrandFaint: "rgba(105,194,184,0.22)",
    stateFocusBg: "rgba(105,194,184,0.06)",
    textFaint: "#37423f",
    brandBg: "rgba(105,194,184,0.1)"
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

function statusColor(status) {
    if (status === "working") return colors.dotWorking
    if (status === "error") return colors.dotError
    if (status === "success" || status === "done") return colors.dotOk
    if (status === "warn") return colors.dotWarn
    if (status === "live" || status === "active") return colors.dotLive
    return colors.dotIdle  // idle
}
