"""Recolour the existing hero background, preserving its illustration geometry."""
from collections import deque
from pathlib import Path
from PIL import Image

assets = Path(__file__).resolve().parents[1] / 'src/assets/companion'
for dark in (False, True):
    suffix = '-dark' if dark else ''
    image = Image.open(assets / f'home-art{suffix}.png').convert('RGB')
    pixels = image.load()
    width, height = image.size
    seen = set()
    queue = deque([(0, 0)])
    while queue:
        x, y = queue.popleft()
        if (x, y) in seen or not (0 <= x < width and 0 <= y < height):
            continue
        seen.add((x, y))
        r, g, b = pixels[x, y]
        if not (r > 190 and g > 145 and b < 150 and r > g):
            continue
        pixels[x, y] = (31, 69, 88) if dark else (218, 239, 245)
        queue.extend(((x-1,y),(x+1,y),(x,y-1),(x,y+1)))
    for y in range(height):
        for x in range(width):
            r, g, b = pixels[x, y]
            if dark and y > 235 and abs(r-25) + abs(g-30) + abs(b-27) < 18:
                pixels[x,y] = (24, 53, 70)
            elif not dark and y > 235 and r > 220 and g > 220 and b > 210:
                pixels[x,y] = (235, 246, 249)
    image.save(assets / f'home-art-glass{suffix}.png', optimize=True)
