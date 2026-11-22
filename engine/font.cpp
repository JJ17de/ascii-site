// font.cpp - 5x7 LED-style glyphs and banner rendering.
#include "font.hpp"

#include <cstddef>

#include "json.hpp"

namespace asciigen {

namespace {

// clang-format off
const std::vector<Glyph> kGlyphs = {
    {' ', false, {"     ", "     ", "     ", "     ", "     ", "     ", "     "}},
    {'A', false, {" ### ", "#   #", "#   #", "#####", "#   #", "#   #", "#   #"}},
    {'B', false, {"#### ", "#   #", "#   #", "#### ", "#   #", "#   #", "#### "}},
    {'C', false, {" ### ", "#   #", "#    ", "#    ", "#    ", "#   #", " ### "}},
    {'D', false, {"#### ", "#   #", "#   #", "#   #", "#   #", "#   #", "#### "}},
    {'E', false, {"#####", "#    ", "#    ", "#### ", "#    ", "#    ", "#####"}},
    {'F', false, {"#####", "#    ", "#    ", "#### ", "#    ", "#    ", "#    "}},
    {'G', false, {" ### ", "#   #", "#    ", "# ###", "#   #", "#   #", " ####"}},
    {'H', false, {"#   #", "#   #", "#   #", "#####", "#   #", "#   #", "#   #"}},
    {'I', false, {" ### ", "  #  ", "  #  ", "  #  ", "  #  ", "  #  ", " ### "}},
    {'J', false, {"  ###", "   # ", "   # ", "   # ", "   # ", "#  # ", " ##  "}},
    {'K', false, {"#   #", "#  # ", "# #  ", "##   ", "# #  ", "#  # ", "#   #"}},
    {'L', false, {"#    ", "#    ", "#    ", "#    ", "#    ", "#    ", "#####"}},
    {'M', false, {"#   #", "## ##", "# # #", "# # #", "#   #", "#   #", "#   #"}},
    {'N', false, {"#   #", "#   #", "##  #", "# # #", "#  ##", "#   #", "#   #"}},
    {'O', false, {" ### ", "#   #", "#   #", "#   #", "#   #", "#   #", " ### "}},
    {'P', false, {"#### ", "#   #", "#   #", "#### ", "#    ", "#    ", "#    "}},
    {'Q', false, {" ### ", "#   #", "#   #", "#   #", "# # #", "#  # ", " ## #"}},
    {'R', false, {"#### ", "#   #", "#   #", "#### ", "# #  ", "#  # ", "#   #"}},
    {'S', false, {" ####", "#    ", "#    ", " ### ", "    #", "    #", "#### "}},
    {'T', false, {"#####", "  #  ", "  #  ", "  #  ", "  #  ", "  #  ", "  #  "}},
    {'U', false, {"#   #", "#   #", "#   #", "#   #", "#   #", "#   #", " ### "}},
    {'V', false, {"#   #", "#   #", "#   #", "#   #", "#   #", " # # ", "  #  "}},
    {'W', false, {"#   #", "#   #", "#   #", "# # #", "# # #", "# # #", " # # "}},
    {'X', false, {"#   #", "#   #", " # # ", "  #  ", " # # ", "#   #", "#   #"}},
    {'Y', false, {"#   #", "#   #", " # # ", "  #  ", "  #  ", "  #  ", "  #  "}},
    {'Z', false, {"#####", "    #", "   # ", "  #  ", " #   ", "#    ", "#####"}},
    {'0', false, {" ### ", "#   #", "#  ##", "# # #", "##  #", "#   #", " ### "}},
    {'1', false, {"  #  ", " ##  ", "  #  ", "  #  ", "  #  ", "  #  ", " ### "}},
    {'2', false, {" ### ", "#   #", "    #", "   # ", "  #  ", " #   ", "#####"}},
    {'3', false, {"#####", "   # ", "  #  ", "   # ", "    #", "#   #", " ### "}},
    {'4', false, {"   # ", "  ## ", " # # ", "#  # ", "#####", "   # ", "   # "}},
    {'5', false, {"#####", "#    ", "#### ", "    #", "    #", "#   #", " ### "}},
    {'6', false, {"  ## ", " #   ", "#    ", "#### ", "#   #", "#   #", " ### "}},
    {'7', false, {"#####", "    #", "   # ", "  #  ", " #   ", " #   ", " #   "}},
    {'8', false, {" ### ", "#   #", "#   #", " ### ", "#   #", "#   #", " ### "}},
    {'9', false, {" ### ", "#   #", "#   #", " ####", "    #", "   # ", " ##  "}},
    {'.', true,  {"     ", "     ", "     ", "     ", "     ", " ##  ", " ##  "}},
    {',', true,  {"     ", "     ", "     ", "     ", " ##  ", " ##  ", "#    "}},
    {'-', true,  {"     ", "     ", "     ", "#### ", "     ", "     ", "     "}},
    {'/', true,  {"    #", "    #", "   # ", "  #  ", " #   ", "#    ", "#    "}},
    {'&', true,  {" ##  ", "#  # ", "# #  ", " #   ", "# # #", "#  # ", " ## #"}},
    {'+', true,  {"     ", "  #  ", "  #  ", "#####", "  #  ", "  #  ", "     "}},
    {'\'', true, {"  #  ", "  #  ", " #   ", "     ", "     ", "     ", "     "}},
    {':', true,  {"     ", " ##  ", " ##  ", "     ", " ##  ", " ##  ", "     "}},
    {'(', true,  {"   # ", "  #  ", " #   ", " #   ", " #   ", "  #  ", "   # "}},
    {')', true,  {" #   ", "  #  ", "   # ", "   # ", "   # ", "  #  ", " #   "}},
    {'!', true,  {"  #  ", "  #  ", "  #  ", "  #  ", "  #  ", "     ", "  #  "}},
    {'?', false, {" ### ", "#   #", "    #", "   # ", "  #  ", "     ", "  #  "}},
};
// clang-format on

void appendBlank(Banner& rows, int cols) {
    for (auto& r : rows) r.append(static_cast<std::size_t>(cols), ' ');
}

}  // namespace

const std::vector<Glyph>& fontGlyphs() { return kGlyphs; }

const Glyph* findGlyph(char ch) {
    for (const auto& g : kGlyphs) {
        if (g.ch == ch) return &g;
    }
    return nullptr;
}

std::string toUpperAscii(const std::string& s) {
    std::string out = s;
    for (auto& c : out) {
        if (c >= 'a' && c <= 'z') c = static_cast<char>(c - 'a' + 'A');
    }
    return out;
}

Banner renderBanner(const std::string& text, std::vector<char32_t>* unknown) {
    Banner rows(kGlyphHeight);
    bool first = true;
    std::size_t i = 0;
    while (i < text.size()) {
        char32_t cp = 0;
        i += utf8Decode(text, i, cp);
        if (!first) appendBlank(rows, kGlyphGap);
        first = false;

        if (cp == U' ') {
            appendBlank(rows, kSpaceWidth);
            continue;
        }
        const Glyph* g = nullptr;
        if (cp < 0x80) {
            char ch = static_cast<char>(cp);
            if (ch >= 'a' && ch <= 'z') ch = static_cast<char>(ch - 'a' + 'A');
            g = findGlyph(ch);
        }
        if (g == nullptr) {
            if (unknown) unknown->push_back(cp);
            g = findGlyph('?');
        }

        int lo = 0;
        int hi = kGlyphWidth - 1;
        if (g->proportional) {
            // Narrow punctuation: keep only the inked columns.
            lo = kGlyphWidth;
            hi = -1;
            for (int r = 0; r < kGlyphHeight; ++r) {
                for (int c = 0; c < kGlyphWidth; ++c) {
                    if (g->rows[r][c] == '#') {
                        if (c < lo) lo = c;
                        if (c > hi) hi = c;
                    }
                }
            }
            if (hi < lo) { lo = 0; hi = kGlyphWidth - 1; }
        }
        for (int r = 0; r < kGlyphHeight; ++r) {
            rows[static_cast<std::size_t>(r)].append(g->rows[r] + lo,
                                                     static_cast<std::size_t>(hi - lo + 1));
        }
    }

    // Remove trailing all-blank columns.
    std::size_t width = 0;
    for (const auto& r : rows) {
        const std::size_t p = r.find_last_of('#');
        if (p != std::string::npos && p + 1 > width) width = p + 1;
    }
    for (auto& r : rows) r.resize(width);
    return rows;
}

}  // namespace asciigen
