from PIL import Image, ImageDraw

# ── Colors ────────────────────────────────────────────────────────────────────
BG    = "#1a1a1a"
PINK  = "#FF6E9C"
GREEN = "#A8FF78"

# ── Canvas ────────────────────────────────────────────────────────────────────
SIZE = 512
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
d   = ImageDraw.Draw(img)

# ── Dark rounded background ───────────────────────────────────────────────────
d.rounded_rectangle(
    [0, 0, SIZE, SIZE],
    radius=80,
    fill=BG
)

# ── Green checkmark (drawn manually) ─────────────────────────────────────────
stroke = 40
d.line(
    [120, 270, 210, 370],
    fill=GREEN,
    width=stroke
)
d.line(
    [210, 370, 390, 150],
    fill=GREEN,
    width=stroke
)

# ── Pink border ───────────────────────────────────────────────────────────────
d.rounded_rectangle(
    [6, 6, SIZE - 6, SIZE - 6],
    radius=80,
    outline=PINK,
    width=16
)

# ── Save ──────────────────────────────────────────────────────────────────────
img.save("taskr_icon.png")
img.save("taskr_icon.ico", format="ICO", sizes=[(512, 512), (256, 256), (128, 128), (64, 64), (32, 32)])
print("Done! taskr_icon.png and taskr_icon.ico saved.")