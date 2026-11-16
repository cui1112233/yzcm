"""Register eyelids using each character's own painted closed eye.

Run after prepare-login-art.py. Never replace shaded fur with a flat colour.
"""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageOps
import cv2
import numpy as np


def prepare_blinks(directory):
    pair = Image.open(directory / "pair.webp").convert("RGBA")
    # Coordinates refer to the 700px registered pair canvas. The source eye
    # already belongs to the same character, including fur shading and lashes.
    for name, source, target, erase in [
        ("blink-girl", (305, 181, 358, 204), (231, 159), (229, 146, 283, 195)),
        ("blink-boy", (314, 315, 365, 334), (231, 315), (228, 299, 283, 344)),
    ]:
        # Reconstruct local shaded fur, rather than copying the opposite cheek
        # (which also copies the mouth/paws) or painting a flat white disc.
        mask = Image.new("L", pair.size)
        ImageDraw.Draw(mask).ellipse(erase, fill=255)
        repaired = cv2.inpaint(np.array(pair.convert("RGB")), np.array(mask), 5, cv2.INPAINT_NS)
        repaired = Image.fromarray(repaired).convert("RGBA")
        layer = repaired
        layer.putalpha(mask.filter(ImageFilter.GaussianBlur(1)))
        eyelid = ImageOps.mirror(pair.crop(source))
        pixels = np.array(eyelid)
        ink = ((pixels[:, :, 0] < 165) & (pixels[:, :, 1] < 110) & (pixels[:, :, 2] < 115)).astype("uint8") * 255
        eyelid.putalpha(Image.fromarray(ink).filter(ImageFilter.GaussianBlur(.4)))
        layer.alpha_composite(eyelid, target)
        layer.save(directory / f"{name}.webp", lossless=True, method=6)


if __name__ == "__main__":
    prepare_blinks(Path(__file__).resolve().parents[1] / "src/assets/login")
