#!/usr/bin/env python3
"""Render exported Go model views with a real monospace font (requires Pillow).

These are model previews, not screenshots of a running service or terminal app.
No network, configuration or system service is touched.
"""
import argparse
import os
from pathlib import Path
import re
import subprocess
import unicodedata


def palette(index):
    base = ["#000000", "#800000", "#008000", "#808000", "#000080", "#800080", "#008080", "#c0c0c0",
            "#808080", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff"]
    if index < 16:
        return base[index]
    if index >= 232:
        return (8 + (index - 232) * 10,) * 3
    index -= 16
    cube = [0, 95, 135, 175, 215, 255]
    return cube[index // 36], cube[index // 6 % 6], cube[index % 6]


def render(source, font_path, font_size):
    from PIL import Image, ImageDraw, ImageFont
    font = ImageFont.truetype(str(font_path), font_size)
    bold_font = font
    if font_path.suffix.lower() == ".ttc":
        for index in range(1, 8):
            try:
                candidate = ImageFont.truetype(str(font_path), font_size, index=index)
            except OSError:
                break
            if candidate.getname()[1] == "Bold":
                bold_font = candidate
                break
    elif font_path.with_name(font_path.stem + "-Bold.ttf").is_file():
        bold_font = ImageFont.truetype(str(font_path.with_name(font_path.stem + "-Bold.ttf")), font_size)
    cell_w = round(font.getlength("M"))
    ascent, descent = font.getmetrics()
    cell_h = ascent + descent + 3
    value = source.read_text()
    plain = re.sub(r"\x1b\[[0-9;:]*m", "", value)
    width = max(sum(2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in line)
                for line in plain.split("\n"))
    height = len(plain.split("\n"))
    image = Image.new("RGB", (width * cell_w, height * cell_h), "#303841")
    draw = ImageDraw.Draw(image)
    x = y = 0
    fg, bg = "#d8dee9", "#303841"
    underline = bold = False
    for token in re.split(r"(\x1b\[[0-9;:]*m)", value):
        if token.startswith("\x1b["):
            p = [int(v or 0) for v in token[2:-1].split(";")]
            i = 0
            while i < len(p):
                n = p[i]
                if n == 0:
                    fg, bg, underline, bold = "#d8dee9", "#303841", False, False
                elif n in (1, 22):
                    bold = n == 1
                elif n in (4, 24):
                    underline = n == 4
                elif n == 39:
                    fg = "#d8dee9"
                elif n == 49:
                    bg = "#303841"
                elif n in (38, 48) and i + 1 < len(p):
                    if p[i + 1] == 2 and i + 4 < len(p):
                        color = tuple(p[i + 2:i + 5])
                        i += 4
                    elif p[i + 1] == 5 and i + 2 < len(p):
                        color = palette(p[i + 2])
                        i += 2
                    else:
                        raise ValueError("unsupported color sequence")
                    if n == 38:
                        fg = color
                    else:
                        bg = color
                elif 30 <= n <= 37 or 90 <= n <= 97:
                    fg = palette(n - 30 if n < 90 else n - 90 + 8)
                elif 40 <= n <= 47 or 100 <= n <= 107:
                    bg = palette(n - 40 if n < 100 else n - 100 + 8)
                i += 1
            continue
        for c in token:
            if c == "\n":
                x, y = 0, y + cell_h
                continue
            cells = 2 if unicodedata.east_asian_width(c) in "WF" else 1
            draw.rectangle((x, y, x + cells * cell_w - 1, y + cell_h - 1), fill=bg)
            draw.text((x, y + 1), c, font=bold_font if bold else font, fill=fg)
            if underline and c != " ":
                draw.line((x, y + ascent + 2, x + cells * cell_w - 1, y + ascent + 2), fill=fg)
            x += cells * cell_w
    image.save(source.with_suffix(".png"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="Explicit model-preview output directory")
    parser.add_argument("--font", type=Path, help="A real monospace TTF/TTC font")
    parser.add_argument("--font-size", type=int, default=18)
    args = parser.parse_args()
    fonts = [args.font] if args.font else [Path("/System/Library/Fonts/Menlo.ttc"),
            Path("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf")]
    font = next((p for p in fonts if p.is_file()), None)
    if font is None:
        parser.error("Pass --font with a monospace TTF/TTC file")
    # Fail before exporting files if this optional preview dependency is absent.
    from PIL import Image  # noqa: F401
    root = Path(__file__).resolve().parent.parent
    output = args.output.resolve()
    env = {**os.environ, "LAZYCLASH_THEME_PREVIEW_DIR": str(output)}
    subprocess.run(["go", "test", "./internal/tui", "-run", "^TestExportMarianaViews$", "-count=1"],
                   cwd=root, env=env, check=True)
    for source in sorted(output.glob("*.ansi")):
        render(source, font, args.font_size)
        print(source.with_suffix(".png"))


if __name__ == "__main__":
    main()
