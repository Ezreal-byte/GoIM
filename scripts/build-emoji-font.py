"""Build the Linux color fallback from the bundled Microsoft Fluent Emoji PNGs.

Run with Python and fonttools installed from the root of the GoIM checkout.
"""
import re
import struct
from pathlib import Path

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib import newTable
from fontTools.ttLib.tables._c_m_a_p import CmapSubtable
from fontTools.ttLib.tables.sbixGlyph import Glyph
from fontTools.ttLib.tables.sbixStrike import Strike

desktop = Path("internal/desktop")
choices = re.findall(r'\{"([^"]+)", "[^"]+", "([^"]+)"\}', (desktop / "emoji_art.go").read_text("utf-8"))
assert choices
family = "GoIM Fluent Emoji"
glyph_order = [".notdef"] + [name for name, _ in choices]
cmap = {ord(value[0]): name for name, value in choices}
assert len(cmap) == len(choices)
builder = FontBuilder(1000, isTTF=True)
builder.setupGlyphOrder(glyph_order)
builder.setupCharacterMap(cmap)
selectors = CmapSubtable.newSubtable(14)
selectors.platformID, selectors.platEncID, selectors.language = 0, 5, 255
selectors.cmap = {}
selectors.uvsDict = {0xFE0F: [(code, None) for code in sorted(cmap)]}
builder.font["cmap"].tables.append(selectors)
builder.setupGlyf({g: TTGlyphPen(None).glyph() for g in glyph_order})
builder.setupHorizontalMetrics({g: (1000, 0) for g in glyph_order})
builder.setupHorizontalHeader(ascent=850, descent=-150)
builder.setupNameTable({
    "familyName": family, "styleName": "Regular", "psName": "GoIMFluentEmoji",
    "copyright": "Artwork Copyright (c) Microsoft Corporation. MIT License.",
    "licenseDescription": "MIT License",
})
builder.setupOS2(sTypoAscender=850, sTypoDescender=-150, usWinAscent=850, usWinDescent=150)
builder.setupPost(keepGlyphNames=False)
table = newTable("sbix")
strike = Strike(ppem=80, resolution=72)
for name, _ in choices:
    image = (desktop / "emoji" / (name + ".png")).read_bytes()
    assert struct.unpack_from(">II", image, 16) == (80, 80)
    strike.glyphs[name] = Glyph(glyphName=name, originOffsetY=-12, graphicType="png ", imageData=image)
table.strikes[80] = strike
builder.font["sbix"] = table
builder.font.recalcTimestamp = False
builder.save(desktop / "emoji" / "fallback.ttf")
