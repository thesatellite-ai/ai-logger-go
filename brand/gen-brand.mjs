// ailog brand kit generator — single source of truth for the identity.
// Run: node gen-brand.mjs  → writes all SVG marks + design tokens here.
// Rasterize with the sibling Taskfile (`task raster`). Values mirror brand.json.
import { writeFileSync } from "node:fs"
import { dirname } from "node:path"
import { fileURLToPath } from "node:url"

const OUT = dirname(fileURLToPath(import.meta.url))

// ── Manifest (from brand.json) ───────────────────────────────────────────────
const M = {
  name: "ailog",
  tile: ["#FB923C", "#EA580C"],
  fg: "#FFFFFF",
  accent: "#22D3EE",
  radius: 112,
  fonts: { display: "'Space Grotesk', 'Geist', ui-sans-serif, system-ui, sans-serif" },
  dark: ["#C2410C", "#431407"], darkFg: "#FFEDD5",
  light: ["#FFF7ED", "#FFEDD5"], lightFg: "#C2410C", lightAccent: "#0891B2",
  glyphAccent: "#0891B2",
}

// ── GLYPH: timeline-log — a thread of captured entries, one highlighted ───────
// orbit:true draws the timeline spine + nodes; favicon (orbit:false) drops them
// for 16px legibility (just 3 entry bars, middle one accent).
function GLYPH(fg, accent, { orbit = true } = {}) {
  if (!orbit) {
    return `<g fill="${fg}">
    <rect x="120" y="138" width="272" height="40" rx="20"/>
    <rect x="120" y="236" width="200" height="40" rx="20" fill="${accent}"/>
    <rect x="120" y="334" width="240" height="40" rx="20"/>
  </g>`
  }
  return `<line x1="142" y1="158" x2="142" y2="354" stroke="${fg}" stroke-width="20" stroke-linecap="round"/>
  <g fill="${fg}">
    <circle cx="142" cy="158" r="20"/>
    <circle cx="142" cy="354" r="20"/>
    <rect x="196" y="138" width="206" height="40" rx="20"/>
    <rect x="196" y="236" width="166" height="40" rx="20"/>
    <rect x="196" y="334" width="186" height="40" rx="20"/>
  </g>
  <circle cx="142" cy="256" r="23" fill="${accent}"/>`
}

// ── Generic machinery ────────────────────────────────────────────────────────
const grad = (id, a, b, x2 = 512, y2 = 512) =>
  `<linearGradient id="${id}" x1="0" y1="0" x2="${x2}" y2="${y2}" gradientUnits="userSpaceOnUse"><stop stop-color="${a}"/><stop offset="1" stop-color="${b}"/></linearGradient>`
const open = (w, h, label) =>
  `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" fill="none" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${label}">`
const W = (f, s) => writeFileSync(`${OUT}/${f}`, s)
const iconFile = (id, a, b, fg, accent, opts) =>
  `${open(512, 512, M.name)}
  <defs>${grad(id, a, b)}</defs>
  <rect width="512" height="512" rx="${M.radius}" fill="url(#${id})"/>
  ${GLYPH(fg, accent, opts)}
</svg>
`

const n = M.name
W(`${n}-icon.svg`,       iconFile("g-color", M.tile[0], M.tile[1], M.fg, M.accent))
W("icon.svg",            iconFile("g-c2",    M.tile[0], M.tile[1], M.fg, M.accent))
W(`${n}-icon-dark.svg`,  iconFile("g-dark",  M.dark[0], M.dark[1], M.darkFg, M.accent))
W(`${n}-icon-light.svg`, iconFile("g-light", M.light[0], M.light[1], M.lightFg, M.lightAccent))
W("favicon.svg",         iconFile("g-fav",   M.tile[0], M.tile[1], M.fg, M.accent, { orbit: false }))
W(`${n}-glyph.svg`,      `${open(512, 512, `${n} glyph`)}\n  ${GLYPH(M.lightFg, M.glyphAccent)}\n</svg>\n`)
W(`${n}-mono.svg`,       `${open(512, 512, n)}\n  ${GLYPH("currentColor", "currentColor")}\n</svg>\n`)

const FONT = M.fonts.display
W(`${n}-wordmark.svg`, `${open(320, 140, n)}\n  <text x="0" y="104" font-family="${FONT}" font-size="132" font-weight="600" letter-spacing="-6" fill="#0B0B12">${n}</text>\n</svg>\n`)

const lockup = (id, textFill) =>
  `${open(620, 200, n)}
  <defs>${grad(id, M.tile[0], M.tile[1])}</defs>
  <g transform="translate(20,36) scale(0.25)"><rect width="512" height="512" rx="${M.radius}" fill="url(#${id})"/>${GLYPH(M.fg, M.accent)}</g>
  <text x="176" y="132" font-family="${FONT}" font-size="116" font-weight="600" letter-spacing="-5" fill="${textFill}">${n}</text>
</svg>
`
W(`${n}-lockup.svg`,      lockup("l-color", "#0B0B12"))
W(`${n}-lockup-dark.svg`, lockup("l-dark", M.light[0]))

const TAGLINE = "A local, searchable log of every AI prompt."
const SUBLINE = "Claude Code · Codex · opencode · local-first · free."
const og = (file, bgA, bgB, main, sub1, sub2, line) =>
  W(file, `${open(1200, 630, n)}
  <defs>${grad("og-bg", bgA, bgB, 1200, 630)}${grad("og-ic", M.tile[0], M.tile[1])}</defs>
  <rect width="1200" height="630" fill="url(#og-bg)"/>
  <g transform="translate(96,180) scale(0.52)"><rect width="512" height="512" rx="${M.radius}" fill="url(#og-ic)"/>${GLYPH(M.fg, M.accent)}</g>
  <text x="412" y="292" font-family="${FONT}" font-size="132" font-weight="600" letter-spacing="-5" fill="${main}">${n}</text>
  <rect x="416" y="324" width="86" height="8" rx="4" fill="${line}"/>
  <text x="414" y="392" font-family="${FONT}" font-size="38" font-weight="500" fill="${sub1}">${TAGLINE}</text>
  <text x="414" y="444" font-family="${FONT}" font-size="27" font-weight="400" fill="${sub2}">${SUBLINE}</text>
</svg>
`)
og("og-cover.svg",       M.dark[0], M.dark[1], "#FFFFFF", M.light[1], "#FDBA74", M.accent)
og("og-cover-light.svg", M.light[0], M.light[1], M.dark[1], M.dark[0], M.lightFg, M.lightFg)

W("tokens.css",
`:root{
  --ailog-tile-a:${M.tile[0]}; --ailog-tile-b:${M.tile[1]};
  --ailog-accent:${M.accent}; --ailog-fg:${M.fg};
  --ailog-font-display:${FONT};
  --ailog-font-sans:'Inter', ui-sans-serif, system-ui, sans-serif;
  --ailog-font-mono:'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  --ailog-radius:16px;
}
[data-theme="dark"], .dark{ --ailog-bg:${M.dark[1]}; --ailog-fg:${M.light[0]}; }
`)
W("palette.json", `${JSON.stringify({ name: n, tile: M.tile, accent: M.accent, fg: M.fg, dark: M.dark, light: M.light, glyphAccent: M.glyphAccent }, null, 2)}\n`)
W("tokens.json", `${JSON.stringify({ name: n, color: { tile: M.tile, accent: M.accent }, font: { display: "Space Grotesk", sans: "Inter", mono: "JetBrains Mono" }, radius: { tile: `${M.radius}@512` }, icon: { favicon: [16, 32, 48, 180, 512], og: [1200, 630] } }, null, 2)}\n`)

console.log(`✓ ${n} brand kit written to ${OUT}`)
