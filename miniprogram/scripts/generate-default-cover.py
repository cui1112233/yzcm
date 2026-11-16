from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageFont

out = Path(__file__).resolve().parents[1] / "src/assets/default-cover.png"
w, h = 280, 360
im = Image.new("RGB", (w, h), "#44545a")
p = im.load()
for y in range(h):
    t = y / h
    for x in range(w):
        glow = max(0, 1 - (((x - 210) / 120) ** 2 + ((y - 70) / 100) ** 2))
        p[x, y] = (int(174 - 75 * t + 38 * glow), int(183 - 78 * t + 34 * glow), int(164 - 70 * t + 20 * glow))
d = ImageDraw.Draw(im, "RGBA")
font = ImageFont.truetype("/System/Library/Fonts/STHeiti Medium.ttc", 24, index=0)
d.polygon([(0, 175), (62, 112), (108, 168), (171, 82), (280, 184), (280, 260), (0, 260)], fill=(58, 78, 78, 220))
d.polygon([(0, 226), (76, 148), (126, 218), (211, 125), (280, 225), (280, 310), (0, 310)], fill=(36, 55, 59, 225))
d.polygon([(0, 293), (280, 248), (280, 360), (0, 360)], fill=(31, 39, 39, 240))
d.ellipse((191, 35, 245, 89), fill=(241, 218, 165, 230))
d.line((0, 304, 280, 274), fill=(204, 182, 129, 100), width=3)
d.line((80, 305, 80, 264), fill=(39, 48, 46, 210), width=4)
d.line((66, 278, 80, 261), fill=(39, 48, 46, 210), width=3)
d.line((94, 280, 80, 261), fill=(39, 48, 46, 210), width=3)
d.text((22, 24), "一战晟铭", font=font, fill=(255, 249, 229, 220))
d.text((22, 318), "个人作品", font=font, fill=(255, 249, 229, 220))
im.filter(ImageFilter.GaussianBlur(.25)).save(out, optimize=True)
