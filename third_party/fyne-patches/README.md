# Loop's Fyne patch

Based on upstream **fyne.io/fyne/v2 v2.8.2**, commit
`77c1fa8e0a50b6cbd04cd6c4454289e55a307a2d`.
The repository stores `loop.patch`, the pinned version/module checksum in
`upstream.json`, and the upstream LICENSE/AUTHORS notices. The root module
replaces Fyne with `third_party/fyne`, a generated
directory ignored by Git. Complete upstream sources and licenses are reconstructed
from Go's verified module cache; no shared module-cache files are modified.

Run `make prepare` once after checkout before direct Go commands or opening the
project in an IDE. `make build`, `make run`, `make test`, `make test-headless`,
`make vet` and the packaging script prepare it automatically. The first run may
download the pinned module; later runs reuse the cache. Go and Git are required.
The helper itself uses only Go's standard library, so it can run before the local
Fyne replacement exists. App packaging retains the upstream LICENSE/AUTHORS and
copies this notice as `fyne-PATCHES.md`.

To change Fyne, edit the generated source, then run `make update-fyne-patch` to
save those changes into `loop.patch`. Edit this README in the patch directory.
Review the patch and run `make test`. A normal prepare verifies the generated
tree and refuses to overwrite unsaved edits. It constructs the next tree in a
temporary directory and keeps the current tree if download, checksum or patch
application fails. Updating Fyne requires changing both `go.mod` and
`upstream.json`, rebasing the patch, and re-running the memory/native UI checks.
If an interrupted process leaves `third_party/.fyne-prepare.lock`, verify no
prepare process is running before removing that empty lock directory and retrying.

Changes cover optional resource interfaces in `resource.go`, the painter's
font cache and text dispatch, and native label truncation in `widget/richtext.go`.
`go.mod` marks the existing grapheme iterator as a direct dependency.

- `SystemFontResource` opts a resource into the macOS CoreText backend using
  its installed PostScript name. Both measuring and RGBA rasterization use
  CoreText; GL and software painters share the same dispatch. PingFang, Menlo,
  symbols and system emoji fallback no longer need Go font-table copies. Other
  resources/platforms retain the portable renderer and the caches below.
- Native label truncation measures with CoreText and splits on grapheme
  boundaries, preserving flags, ZWJ emoji and combining marks without loading
  a second Go shaping engine. Tabs preserve Fyne stops; monospace ligatures
  are disabled. Underline/strike rendering stays with Fyne.
- The native cache retains at most 48 font objects, keyed by name/size/italic,
  protected by a mutex; each active call retains its font across eviction.
  Lines, attributed strings, contexts and color spaces are released per call.
  RGBA buffers are borrowed synchronously; no Go pointer escapes to C. Native
  fonts and their files are never copied into the app bundle.

- Share immutable `font.Font` tables by SHA-256 content hash across resources,
  styles and theme resets. Keep distinct mutable `font.Face` objects for glyph
  caches, sizes and variations. Parsing is synchronized and the cache has an
  LRU limit of 16 entries; resource names alone never identify font contents.
- Resolve the system locale fallback on the first missing glyph, rather than
  eagerly parsing it for every theme font. Explicit PingFang and ASCII-only
  monospace text need no additional CJK face. Missing glyphs still use locale
  and system fallback, preserving font weight/style.
- `FontFileResource` is an optional Loop extension of `fyne.Resource` that
  identifies a local font file and collection index. The painter reads the
  selected face's tables directly, avoiding a retained standalone SFNT copy.
  File cache keys include absolute path, index, size and modification time;
  normal byte resources still use content hashes. Readers are closed after
  parsing. `Content` remains available for other resource consumers.
- Key font maps by the selected font resources and fallback style, rather than
  color/size theme scope IDs. Matching scopes reuse their glyph caches; different
  fonts and fallback styles remain distinct. Mutable faces are still separate
  between distinct font maps.
- Load fallback outlines/shaping tables only when needed. Small character maps
  decide coverage and preserve fallback order without parsing the complete font.
  Monospace text can reuse the theme's file-backed CJK face, avoiding another
  system-font parse for Chinese JSON values. Emoji remain available on demand.

This addresses repeated parsing/retention of large CJK and emoji tables during
page rendering and appearance changes. It does not remove fonts or emoji.
Remove this replacement when an upstream release provides equivalent behavior;
do not drop it during a routine dependency update without re-measuring memory.

Native tests cover no Content/Go-table parsing, Chinese/emoji/styles, alpha,
orientation, tab/grid advances, clipped textures at 1x/1.5x/2x, truncation and
concurrent font eviction. App tests cover wrapped/truncated labels and Entry
hit-testing against native text advances. Existing portable golden tests remain.

Regression checks from the root:

```sh
make test
```
