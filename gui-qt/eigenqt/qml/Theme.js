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
    dotError: "#d67e72"     // error
}

var uiFonts = ["Inter", "Noto Sans", "sans-serif"]
var monoFonts = ["JetBrains Mono", "JetBrainsMono Nerd Font", "DejaVu Sans Mono", "monospace"]

// 4px base spacing scale
var space = {
    xxs: 2,
    xs: 4,
    sm: 6,
    md: 8,
    lg: 12,
    xl: 16,
    xxl: 20,
    xxxl: 24,
    xxxxl: 32
}

var radius = {
    sm: 5,
    md: 6,
    lg: 8
}

var fontSize = {
    display: 28,
    h1: 22,
    h2: 18,
    h3: 15,
    body: 13,
    bodySm: 12,
    label: 12,
    micro: 11,
    code: 13
}

var fontWeight = {
    regular: 400,
    medium: 500,
    semibold: 600,
    bold: 700
}

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
