// font.hpp - 5x7 bitmap font and block-letter banner rendering.
#pragma once

#include <string>
#include <vector>

namespace asciigen {

constexpr int kGlyphWidth = 5;
constexpr int kGlyphHeight = 7;
constexpr int kSpaceWidth = 3;  // columns used by a space character
constexpr int kGlyphGap = 1;    // blank columns between glyphs

// A banner is exactly kGlyphHeight strings of equal length ('#' on, ' ' off).
using Banner = std::vector<std::string>;

struct Glyph {
    char ch;
    bool proportional;  // punctuation: rendered at its ink width, not 5 cols
    const char* rows[kGlyphHeight];
};

// All glyphs in the font (A-Z, 0-9, space, punctuation).
const std::vector<Glyph>& fontGlyphs();

// Returns the glyph for an (already uppercased) ASCII char, or nullptr.
const Glyph* findGlyph(char ch);

// ASCII-only uppercase (no locale dependence).
std::string toUpperAscii(const std::string& s);

// Renders UTF-8 text. Lowercase is uppercased; unsupported code points render
// as '?' and are appended to *unknown (if non-null) so the caller can warn.
Banner renderBanner(const std::string& text, std::vector<char32_t>* unknown);

}  // namespace asciigen
