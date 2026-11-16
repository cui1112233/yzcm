"""Generate local PNG icons; Pillow is a build-time drawing tool only."""
from pathlib import Path
from PIL import Image, ImageDraw

icons = Path(__file__).resolve().parents[1] / "src/assets/icons"
scale = 4
for name in ("search", "plus"):
    image = Image.new("RGBA", (96 * scale, 96 * scale))
    draw = ImageDraw.Draw(image)
    color = (255, 255, 255, 255)
    if name == "search":
        draw.ellipse((17 * scale, 13 * scale, 69 * scale, 65 * scale), outline=color, width=6 * scale)
        draw.line((64 * scale, 60 * scale, 82 * scale, 79 * scale), fill=color, width=6 * scale)
    else:
        draw.line((48 * scale, 20 * scale, 48 * scale, 76 * scale), fill=color, width=6 * scale)
        draw.line((20 * scale, 48 * scale, 76 * scale, 48 * scale), fill=color, width=6 * scale)
    image.resize((96, 96), Image.Resampling.LANCZOS).save(icons / f"{name}.png")

for theme, normal, selected in (("light", "#626D64", "#315C4C"), ("dark", "#A2A5A8", "#FA825F")):
    for tab in ("workspace", "tasks", "functions", "profile"):
        for state, color in (("normal", normal), ("selected", selected)):
            path = icons / f"tab-{tab}-{state}-{theme}.png"
            source = Image.open(path).convert("RGBA")
            image = Image.new("RGBA", source.size, color)
            image.putalpha(source.getchannel("A"))
            image.save(path)
