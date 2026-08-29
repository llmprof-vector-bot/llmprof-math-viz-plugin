# Math Visualization Plugin for LLM Prof

An Extism WASM plugin for [LLM Prof](https://github.com/LukeMitDemHut/llmprof-agentdev) that adds an interactive **step-by-step mathematical formula visualization** tool to the AI chatbot. The plugin renders LaTeX equations using a self-contained KaTeX bundle with smooth animations.

## Features

- **renderFormulaVisualization** tool with three modes: define, execute, applet
- LLM generates mathematical steps dynamically (LaTeX + explanations)
- Self-contained KaTeX rendering (JS + CSS + fonts bundled via `go:embed`)
- Interactive navigation: round step badges, prev/next arrows, mouse wheel scroll, keyboard
- Smooth vertical transition animations with opacity fading
- Previous step visible as faded context above active step
- Final result highlighted in a distinct result card
- Fixed-height applet body (320px) for compact chat integration
- Content returned as LLM prompt (no redundant math in chat text)

## Tool: renderFormulaVisualization

### Parameters

| Parameter | Type | Required | Description |
|----------|------|----------|-------------|
| `title`  | string | yes | Concise title for the derivation |
| `steps`  | array | yes | Ordered sequence of steps (minItems: 1) |
| `steps[].tex` | string | yes | Valid LaTeX code for this step's equation |
| `steps[].note` | string | yes | Brief explanation of the transformation (1-2 sentences) |

### Response

- **content**: A prompt instructing the LLM to refer to the applet, not repeat the math
- **ui.applet_id**: Reference to stored data for applet rendering

## Build

### Prerequisites

- Docker (for the Go build container)

### Build the plugin

```bash
./build.sh
```

This produces `go/plugin.wasm` — the compiled Extism WASM plugin (~5.4MB, larger due to embedded KaTeX assets).

## Install in LLM Prof

```bash
cp go/plugin.wasm go/manifest.json /path/to/llmprof-agentdev/plugins/plugins/math-viz-plugin/
docker compose restart llmprof_extism_plugin_host
./dev console app:manage-plugin-installation install \
  --provider=extism --plugin=math-viz-plugin --plugin-version=1.0.0 \
  --scope=context --scope-target=<context-uuid> --config='{}'
```

## Architecture

```
math-viz-plugin/
├── go/
│   ├── main.go          # Plugin source (~500 lines)
│   ├── manifest.json    # Extism manifest
│   ├── go.mod           # Go module
│   ├── assets/
│   │   ├── katex.min.js    # KaTeX JS (272KB)
│   │   └── katex.min.css   # KaTeX CSS with inlined woff2 fonts (1.4MB)
│   └── build            # Docker-based build script
├── builds/
│   └── plugin.wasm      # Pre-built release artifact
├── screenshots/         # Integration test screenshots
├── build.sh             # Top-level build script
└── README.md            # This file
```

## Technical Decisions

- **Language**: Go — `go:embed` for bundling KaTeX assets, native WASI filesystem for storage
- **KaTeX bundling**: CSS with woff2 fonts inlined as base64 data URIs (woff/ttf dropped for size). JS and CSS embedded in Go binary via `go:embed`, written to `/storage/` on install
- **Storage**: Step data in `/storage/mathviz-{timestamp}.json`, KaTeX assets in `/storage/katex.min.js` and `/storage/katex.min.css`
- **Content as LLM prompt**: Tool returns instructions for the LLM to not repeat math in chat text
- **Applet design**: Fixed 320px height, green accent (#2d7d2d), CSS transforms for animations, KaTeX rendered via `katex.render()`

## Boilerplate

Built against [llmprof-plugin-boilerplate](https://github.com/LukeMitDemHut/llmprof-plugin-boilerplate) commit `11a604c`.

## KaTeX

Uses [KaTeX v0.18.4](https://github.com/KaTeX/KaTeX) (MIT license). Only woff2 fonts included for smaller size.