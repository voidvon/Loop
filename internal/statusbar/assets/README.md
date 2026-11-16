# Loop menu bar template

Generated with the built-in image generation tool, using
`../../desktop/assets/app-icon.png` as the brand reference.

`loop-template-source.png` preserves the generated transparent original.
The production PNGs crop its visible mark and fit it proportionally inside an
18-point square: `loop-template.png` (18×18), `loop-template@2x.png` (36×36),
and `loop-template@3x.png` (54×54). Their alpha is preserved. Cocoa loads them
as representations of one template image and handles system appearance.
When mode letters are enabled, Cocoa shifts the mark up by 1 point and clears
the same lower-right rectangle for every mode (x=10.5, y=0, w=7.5, h=10.5).
G / R / D are drawn into that cutout with the native semibold monospaced font.
The complete composition (mark, cutout and letter) is translated down 1 point
without changing their relative positions.
Each combined template is cached at 1×, 2× and 3× and stays inside the same
18-point square. The status item uses the system square width and no separate
text. Disconnected dimming applies to the combined image. Turning mode letters
off restores the complete, centered infinity mark.

## Generation prompt

Use case: logo-brand. Asset type: production macOS menu bar template icon for Loop. Image 1 is a brand reference, not an edit target. Generate ONE minimal solid-black infinity loop mark on a genuinely transparent background. Derive the wide, horizontal infinity ribbon silhouette from the reference: two generous open loops, smooth confident rounded contours, a clean diagonal crossover. Simplify to a uniform bold black ribbon with crisp flat vector-like edges. At the central crossover a small transparent separation may clarify the over-under ribbon, but must remain legible at only 18 points wide. Aspect of mark about 1.8:1, centered on square transparent canvas with 12 percent horizontal margin. Monochrome black and alpha ONLY. Omit the two separate orbit arrows and omit the app tile, gradients, shine, shading, drop shadows, texture, outline frame, any text, letters or mockup. This is the actual tiny icon asset, not a presentation sheet. Balanced symmetry and open negative space, suitable for native macOS menu bar alongside system icons.
