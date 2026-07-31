# Brand

The wordmark is the product's own name in IBM Plex Mono SemiBold with the block
cursor from the TUI. The mark is the same idea in a square: `am` on a tile, in
the turquoise the TUI uses for its second accent.

Every glyph is outlined rather than live text, so these render identically
wherever the font is not installed.

| File | Use |
|---|---|
| `wordmark-light.svg` | Light backgrounds |
| `wordmark-dark.svg` | Dark backgrounds, including the README in dark mode |
| `mark.svg` | Square contexts: avatars, social cards |
| `mark-512.png` | Raster, for anything that will not take an SVG |

Turquoise is `#12766a` on light and `#6cb6a4` on dark. The second is the TUI's
own `accent2`; it fails contrast on light backgrounds, which is why the light
lockup uses the deeper value.

The full set, plus the script that regenerates these from the font, lives in the
site repo under `brand/`.
