"""Offline regression for shaded, registered eyelids (no runtime dependency)."""
import unittest
from pathlib import Path
import numpy as np
from PIL import Image


class LoginBlinkTests(unittest.TestCase):
    def test_feathered_edges_do_not_contain_black_matte(self):
        root = Path(__file__).resolve().parents[1] / "src/assets/login"
        pair = np.array(Image.open(root / "pair.webp").convert("RGBA"))
        for name in ("blink-boy", "blink-girl"):
            layer = np.array(Image.open(root / f"{name}.webp").convert("RGBA"))
            self.assertEqual(layer.shape, pair.shape)
            feather = (layer[:, :, 3] > 5) & (layer[:, :, 3] < 100) & (pair[:, :, :3].min(axis=2) > 180)
            self.assertGreater(int(feather.sum()), 20)
            # Straight-alpha colour must remain fur-coloured at transparent
            # edges. Premultiplying here adds a grey ring on composition.
            self.assertGreater(float(layer[:, :, :3][feather].mean()), 180, name)


if __name__ == "__main__":
    unittest.main()
