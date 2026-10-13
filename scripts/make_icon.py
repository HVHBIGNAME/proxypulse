"""Generate the ProxyPulse app icon (dark tile + cyan->green pulse)."""
from PIL import Image, ImageDraw

S = 1024
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)

# rounded dark background with subtle vertical gradient
bg = Image.new("RGBA", (S, S), (0, 0, 0, 0))
bd = ImageDraw.Draw(bg)
for y in range(S):
    t = y / S
    r = int(11 + 14 * t)
    g = int(20 + 16 * t)
    b = int(34 + 30 * t)
    bd.line([(0, y), (S, y)], fill=(r, g, b, 255))
mask = Image.new("L", (S, S), 0)
ImageDraw.Draw(mask).rounded_rectangle([0, 0, S - 1, S - 1], radius=int(S * 0.22), fill=255)
img.paste(bg, (0, 0), mask)

d = ImageDraw.Draw(img)

# glow ellipse behind the pulse
glow = Image.new("RGBA", (S, S), (0, 0, 0, 0))
gd = ImageDraw.Draw(glow)
gd.ellipse([S * 0.12, S * 0.30, S * 0.88, S * 0.70], fill=(0, 229, 255, 70))
from PIL import ImageFilter
glow = glow.filter(ImageFilter.GaussianBlur(S * 0.06))
img = Image.alpha_composite(img, glow)
d = ImageDraw.Draw(img)

# ECG / market pulse line
pts = [
    (0.10, 0.55), (0.28, 0.55), (0.36, 0.34), (0.45, 0.74),
    (0.54, 0.28), (0.63, 0.66), (0.72, 0.55), (0.90, 0.55),
]
px = [(x * S, y * S) for x, y in pts]
w = int(S * 0.045)
# outer soft stroke
d.line(px, fill=(0, 200, 255, 90), width=w + int(S * 0.03), joint="curve")
# gradient main stroke cyan -> green by segment
for i in range(len(px) - 1):
    t = i / max(1, len(px) - 2)
    col = (int(0 + 40 * t), int(229 - 20 * t), int(255 - 130 * t), 255)
    d.line([px[i], px[i + 1]], fill=col, width=w, joint="curve")
# bright joints
for p in px:
    r = w * 0.62
    d.ellipse([p[0] - r, p[1] - r, p[0] + r, p[1] + r], fill=(180, 255, 240, 255))

# small "live" dot top-right
d.ellipse([S * 0.70, S * 0.16, S * 0.84, S * 0.30], fill=(57, 255, 138, 255))
d.ellipse([S * 0.735, S * 0.195, S * 0.805, S * 0.265], fill=(255, 255, 255, 255))

img.save("assets/proxypulse_1024.png")
img.save("assets/proxypulse.ico",
         sizes=[(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (16, 16)])
print("icon written")