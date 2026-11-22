// json.hpp - tiny helpers for writing ASCII-only JSON.
#pragma once

#include <cmath>
#include <cstddef>
#include <cstdio>
#include <string>

namespace asciigen {

// Decodes one UTF-8 code point starting at s[i]. Returns the number of bytes
// consumed (always >= 1). Invalid or truncated sequences, overlong forms,
// surrogates and values above U+10FFFF yield U+FFFD and consume one byte.
inline std::size_t utf8Decode(const std::string& s, std::size_t i, char32_t& cp) {
    const std::size_t n = s.size();
    const unsigned char c0 = static_cast<unsigned char>(s[i]);
    if (c0 < 0x80) {
        cp = c0;
        return 1;
    }
    std::size_t len = 0;
    char32_t v = 0;
    char32_t minv = 0;
    if ((c0 & 0xE0) == 0xC0) {
        len = 2; v = c0 & 0x1F; minv = 0x80;
    } else if ((c0 & 0xF0) == 0xE0) {
        len = 3; v = c0 & 0x0F; minv = 0x800;
    } else if ((c0 & 0xF8) == 0xF0) {
        len = 4; v = c0 & 0x07; minv = 0x10000;
    } else {
        cp = 0xFFFD;
        return 1;
    }
    if (i + len > n) {
        cp = 0xFFFD;
        return 1;
    }
    for (std::size_t k = 1; k < len; ++k) {
        const unsigned char ck = static_cast<unsigned char>(s[i + k]);
        if ((ck & 0xC0) != 0x80) {
            cp = 0xFFFD;
            return 1;
        }
        v = (v << 6) | (ck & 0x3F);
    }
    if (v < minv || v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF)) {
        cp = 0xFFFD;
        return 1;
    }
    cp = v;
    return len;
}

namespace json {

// Formats a double with exactly 4 decimals; negative zero becomes 0.0000.
inline std::string number(double v) {
    if (!std::isfinite(v)) return "null";
    char buf[64];
    std::snprintf(buf, sizeof buf, "%.4f", v);
    std::string s(buf);
    if (s == "-0.0000") s = "0.0000";
    return s;
}

inline void appendEscape(std::string& out, unsigned unit) {
    char buf[8];
    std::snprintf(buf, sizeof buf, "\\u%04x", unit & 0xFFFFu);
    out += buf;
}

// Returns s as a quoted JSON string. Output is pure ASCII: non-ASCII code
// points are written as \uXXXX escapes (surrogate pairs above U+FFFF).
inline std::string quote(const std::string& s) {
    std::string out;
    out.reserve(s.size() + 2);
    out += '"';
    std::size_t i = 0;
    while (i < s.size()) {
        char32_t cp = 0;
        i += utf8Decode(s, i, cp);
        switch (cp) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\b': out += "\\b"; break;
            case '\f': out += "\\f"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default:
                if (cp < 0x20 || cp == 0x7F) {
                    appendEscape(out, static_cast<unsigned>(cp));
                } else if (cp < 0x80) {
                    out += static_cast<char>(cp);
                } else if (cp <= 0xFFFF) {
                    appendEscape(out, static_cast<unsigned>(cp));
                } else {
                    const unsigned v = static_cast<unsigned>(cp) - 0x10000u;
                    appendEscape(out, 0xD800u + (v >> 10));
                    appendEscape(out, 0xDC00u + (v & 0x3FFu));
                }
        }
    }
    out += '"';
    return out;
}

}  // namespace json
}  // namespace asciigen
