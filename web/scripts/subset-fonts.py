"""Subset the self-hosted fonts in place. Requires fonttools[woff].

Run from web/: python3 scripts/subset-fonts.py
Retain Latin, Latin Extended, combining marks, punctuation, currency, arrows,
math and UI symbols. Other scripts continue to use the CSS system fallback.
"""
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont

RANGES = (
    (0x0000, 0x024F), (0x0300, 0x036F), (0x1D00, 0x1EFF),
    (0x2000, 0x206F), (0x20A0, 0x23FF), (0x2500, 0x27BF),
    (0x2C60, 0x2C7F), (0xA720, 0xA7FF), (0xAB30, 0xAB6F),
    (0xFEFF, 0xFEFF), (0xFFFD, 0xFFFD),
)
UNICODES = {code for start, end in RANGES for code in range(start, end + 1)}
for path in sorted(Path("public/fonts").glob("*.woff2")):
    font = TTFont(path, recalcTimestamp=False)
    expected = set(font.getBestCmap()) & UNICODES
    options = subset.Options()
    options.flavor = "woff2"
    # Keep standard shaping/kerning and the character variants used by index.css.
    # Omit other optional stylistic sets and hinting.
    options.layout_features += ["cv01", "cv02", "cv03"]
    options.hinting = False
    subsetter = subset.Subsetter(options=options)
    subsetter.populate(unicodes=UNICODES)
    subsetter.subset(font)
    before = path.stat().st_size
    font.save(path)
    actual = set(TTFont(path).getBestCmap())
    assert expected <= actual, f"Lost glyphs: {expected - actual}"
    assert set(map(ord, "Café · − ×")) <= actual, path
    # Each face retains its original UI symbols; missing symbols use the local fallback.
    symbols = set(map(ord, "→ · − ✓ × ⌘ ⇆ ↕ ⌕")) & expected
    assert symbols <= actual, path
    print(f"{path.name}: {before:,} -> {path.stat().st_size:,} bytes; {len(actual)} codepoints; UI glyphs verified")
