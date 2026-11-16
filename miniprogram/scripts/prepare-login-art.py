"""Prepare small, registered native images from the approved local login study.

Usage: NODE_PATH=<sharp modules> python3 scripts/prepare-login-art.py STUDY_DIR RIG_SVG
No credentials, remote downloads, or runtime image-processing dependencies.
"""
import io
import copy
import subprocess
import sys
import xml.etree.ElementTree as ET
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter

study, rig = map(Path, sys.argv[1:3])
out = Path(__file__).resolve().parents[1] / "src/assets/login"
out.mkdir(parents=True, exist_ok=True)

def save(image, name, width=700):
    if image.width > width:
        image = image.resize((width, round(image.height * width / image.width)), Image.Resampling.LANCZOS)
    image.save(out / f"{name}.webp", quality=89, method=6)

def render(svg):
    data = subprocess.run(["node", "-e", "const sharp=require('sharp');let chunks=[];process.stdin.on('data',b=>chunks.push(b));process.stdin.on('end',async()=>{process.stdout.write(await sharp(Buffer.concat(chunks)).png().toBuffer())});"], input=ET.tostring(svg), stdout=subprocess.PIPE, check=True).stdout
    return Image.open(io.BytesIO(data)).convert("RGBA")

for source, name in [("login-layer-box.png", "box"), ("login-layer-parts.png", "parts"), ("login-keychain-boy.png", "charm-boy"), ("login-keychain-girl.png", "charm-girl")]:
    save(Image.open(study / source).convert("RGBA"), name, 120 if name.startswith("charm") else 700)

pair = Image.open(study / "login-layer-pair.png").convert("RGBA")
tail_mask = Image.new("L", pair.size)
ImageDraw.Draw(tail_mask).polygon([(770,210),(944,210),(944,355),(818,355),(788,321)], fill=255)
tail = pair.copy()
tail.putalpha(Image.composite(pair.getchannel("A"), Image.new("L", pair.size), tail_mask))
body = pair.copy()
body.putalpha(Image.composite(Image.new("L", pair.size), pair.getchannel("A"), tail_mask))
save(body, "pair")
save(tail, "tail")
subprocess.run([sys.executable, str(Path(__file__).with_name("prepare-login-blinks.py"))], check=True)

ns="http://www.w3.org/2000/svg"
ET.register_namespace("",ns)
tree=ET.fromstring(rig.read_text())
tree.set("xmlns",ns)
for index, name in enumerate(("boy","girl")):
    art=ET.fromstring(ET.tostring(tree))
    art.attrib.pop("style",None)
    actors=[n for n in art if "pull-actor" in n.get("class","")]
    art.remove(actors[1-index])
    target=actors[index]
    target.set("transform","translate(0 0)")
    for parent in list(art.iter()):
        for child in list(parent):
            if any(c in child.get("class","").split() for c in ["cry-face","boy-scratch","boy-goofy-grin","boy-happy-eye"]):
                parent.remove(child)
    for child in art.iter():
        if any(c in child.get("class","") for c in ["upright-arm-","boy-arm-"]): child.attrib.pop("style",None)
    art.set("viewBox","0 0 160 220")
    art.set("width","320");art.set("height","440")
    style=ET.SubElement(art,f"{{{ns}}}style")
    style.text=".portrait-trace{stroke:#76544b;stroke-width:5.5;stroke-linecap:round;stroke-linejoin:round}.calf-eye,.boy-idle-eye{transform:none}.boy-rest-mouth{display:block}"
    def is_arm(node):
        return any(c in node.get("class", "").split() for c in ("upright-arm-left", "upright-arm-right", "boy-arm-left", "boy-arm-right"))
    arms = copy.deepcopy(art)
    def prune_to_arms(node):
        if is_arm(node): return True
        if node.tag.split("}")[-1] in ("defs", "style"): return True
        found = False
        for child in list(node):
            keep = prune_to_arms(child)
            if not keep: node.remove(child)
            else: found = True
        return found
    prune_to_arms(arms)
    for parent in list(art.iter()):
        for child in list(parent):
            if is_arm(child): parent.remove(child)
    save(render(art),name,320)
    save(render(arms),name+"-arms",320)

# The exact approved web cloud, including its shading and curl highlights.
# Bake the web's 2.5px blur into the bitmap; native filter support varies.
cloud = ET.fromstring('''<svg xmlns="http://www.w3.org/2000/svg" width="540" height="380" viewBox="0 0 270 190"><defs><linearGradient id="smoke-fill" x2=".35" y2="1"><stop stop-color="#fffdfb"/><stop offset="1" stop-color="#e9edf8"/></linearGradient></defs><path d="M35 136C9 134 3 106 23 92C8 73 20 46 45 47C42 20 75 6 96 24C115 2 146 11 154 30C180 8 211 26 210 48C241 36 262 61 251 83C278 101 262 133 239 135C242 162 207 178 186 159C170 184 134 185 118 164C87 185 52 171 53 150C43 151 37 144 35 136Z" fill="url(#smoke-fill)"/><path d="M29 88Q23 62 48 59M90 35Q104 23 118 32M217 62Q237 58 239 77M229 126Q222 145 205 144M69 143Q70 158 89 157" fill="none" stroke="#fff" stroke-width="4" stroke-linecap="round" opacity=".9"/></svg>''')
cloud_pixels=render(cloud)
smoke=Image.alpha_composite(Image.new("RGBA", cloud_pixels.size, "#fffdfb"), cloud_pixels).filter(ImageFilter.GaussianBlur(5))
smoke.putalpha(cloud_pixels.getchannel("A").filter(ImageFilter.GaussianBlur(5)))
save(smoke,"smoke",540)

# Paws are part of the panel's moving coordinate system, at the four actual
# wrists, rather than four evenly spaced detached circles.
paws = ET.fromstring('''<svg xmlns="http://www.w3.org/2000/svg" width="666" height="48" viewBox="0 0 333 24"><defs><linearGradient id="fur" x2="0" y2="1"><stop stop-color="#fff"/><stop offset="1" stop-color="#f1e4ed"/></linearGradient></defs></svg>''')
for x in (66,151,185,262):
    ET.SubElement(paws, f"{{{ns}}}path", {"d":f"M{x-7} 7C{x-12} 7 {x-12} 15 {x-8} 18Q{x} 23 {x+7} 18C{x+12} 14 {x+9} 7 {x+3} 6Z", "fill":"url(#fur)", "stroke":"#76544b", "stroke-width":"1.4"})
    ET.SubElement(paws, f"{{{ns}}}path", {"d":f"M{x-4} 16v3m5-3v3", "stroke":"#cdb6c2", "stroke-width":".8", "fill":"none"})
save(render(paws),"grips",666)
print(f"Prepared {len(list(out.glob('*.webp')))} login layers: {sum(p.stat().st_size for p in out.glob('*.webp')) // 1024} KiB")
