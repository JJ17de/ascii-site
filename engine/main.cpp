// main.cpp - asciigen CLI: banners + constellation -> JSON.
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <exception>
#include <fstream>
#include <set>
#include <string>
#include <utility>
#include <vector>

#include "font.hpp"
#include "json.hpp"
#include "scene.hpp"

namespace {

using namespace asciigen;

const char* const kVersion = "1.0.0";
const char* const kDefaultHeadings =
    "about,experience,skills,certifications,education,projects,contact";

struct Options {
    std::string name;
    bool hasName = false;
    std::string tagline;
    bool hasTagline = false;
    std::string headings = kDefaultHeadings;
    std::uint64_t seed = 2026;
    int nodes = 150;
    std::string out;
    bool hasOut = false;
};

void printUsage(std::FILE* f) {
    std::fprintf(f,
        "usage: asciigen --name \"<TEXT>\" [--tagline \"<TEXT>\"] [--headings \"a,b,c\"]\n"
        "                [--seed N] [--out PATH] [--nodes N]\n"
        "       asciigen --self-test\n"
        "       asciigen --help\n"
        "\n"
        "  --name      text for the main banner (required)\n"
        "  --tagline   optional second banner\n"
        "  --headings  comma-separated section headings\n"
        "              (default: %s)\n"
        "  --seed      unsigned 64-bit seed (default: 2026)\n"
        "  --nodes     constellation node count, %d..%d (default: 150)\n"
        "  --out       output file, written atomically (default: stdout)\n",
        kDefaultHeadings, kMinNodes, kMaxNodes);
}

std::string trim(const std::string& s) {
    const auto b = s.find_first_not_of(" \t\r\n");
    if (b == std::string::npos) return "";
    const auto e = s.find_last_not_of(" \t\r\n");
    return s.substr(b, e - b + 1);
}

bool parseUint64(const std::string& s, std::uint64_t& v) {
    if (s.empty() || s.size() > 20) return false;
    std::uint64_t acc = 0;
    for (char c : s) {
        if (c < '0' || c > '9') return false;
        const std::uint64_t d = static_cast<std::uint64_t>(c - '0');
        if (acc > (UINT64_MAX - d) / 10) return false;
        acc = acc * 10 + d;
    }
    v = acc;
    return true;
}

std::vector<std::string> splitHeadings(const std::string& csv) {
    std::vector<std::string> out;
    std::set<std::string> seen;
    std::size_t start = 0;
    while (start <= csv.size()) {
        std::size_t comma = csv.find(',', start);
        if (comma == std::string::npos) comma = csv.size();
        const std::string h = trim(csv.substr(start, comma - start));
        if (!h.empty() && seen.insert(h).second) out.push_back(h);
        start = comma + 1;
    }
    return out;
}

std::vector<std::string> splitWords(const std::string& s) {
    std::vector<std::string> words;
    std::string cur;
    for (char c : s) {
        if (c == ' ') {
            if (!cur.empty()) words.push_back(cur);
            cur.clear();
        } else {
            cur += c;
        }
    }
    if (!cur.empty()) words.push_back(cur);
    return words;
}

std::string pad(int n) { return std::string(static_cast<std::size_t>(n), ' '); }

void emitBanner(std::string& s, const Banner& b, int indent) {
    s += "[\n";
    for (std::size_t r = 0; r < b.size(); ++r) {
        s += pad(indent + 2) + json::quote(b[r]);
        s += (r + 1 < b.size()) ? ",\n" : "\n";
    }
    s += pad(indent) + "]";
}

struct Document {
    std::string json;
    std::vector<Banner> banners;  // every banner rendered (for self-test)
    Constellation scene;
};

Document buildDocument(const Options& o, std::vector<char32_t>* unknown) {
    Document doc;
    const std::string name = trim(o.name);
    const std::string tagline = trim(o.tagline);
    const bool hasTagline = o.hasTagline && !tagline.empty();

    const Banner nameBanner = renderBanner(name, unknown);
    std::vector<Banner> wordBanners;
    for (const auto& w : splitWords(name)) wordBanners.push_back(renderBanner(w, unknown));
    Banner tagBanner;
    if (hasTagline) tagBanner = renderBanner(tagline, unknown);
    std::vector<std::pair<std::string, Banner>> headings;
    for (const auto& h : splitHeadings(o.headings)) {
        headings.emplace_back(h, renderBanner(toUpperAscii(h), unknown));
    }

    doc.scene = buildConstellation(o.seed, o.nodes);

    std::string& s = doc.json;
    s += "{\n";
    s += "  \"generator\": \"asciigen\",\n";
    s += "  \"version\": " + json::quote(kVersion) + ",\n";
    s += "  \"seed\": " + std::to_string(o.seed) + ",\n";
    s += "  \"font\": {\"width\": " + std::to_string(kGlyphWidth) +
         ", \"height\": " + std::to_string(kGlyphHeight) + "},\n";
    s += "  \"banners\": {\n";
    s += "    \"name\": ";
    emitBanner(s, nameBanner, 4);
    doc.banners.push_back(nameBanner);
    s += ",\n    \"nameWords\": [";
    for (std::size_t i = 0; i < wordBanners.size(); ++i) {
        s += (i == 0) ? "\n      " : ",\n      ";
        emitBanner(s, wordBanners[i], 6);
        doc.banners.push_back(wordBanners[i]);
    }
    s += wordBanners.empty() ? "]" : "\n    ]";
    s += ",\n    \"tagline\": ";
    if (hasTagline) {
        emitBanner(s, tagBanner, 4);
        doc.banners.push_back(tagBanner);
    } else {
        s += "null";
    }
    s += ",\n    \"headings\": {";
    for (std::size_t i = 0; i < headings.size(); ++i) {
        s += (i == 0) ? "\n      " : ",\n      ";
        s += json::quote(headings[i].first) + ": ";
        emitBanner(s, headings[i].second, 6);
        doc.banners.push_back(headings[i].second);
    }
    s += headings.empty() ? "}" : "\n    }";
    s += "\n  },\n";

    const Constellation& c = doc.scene;
    s += "  \"constellation\": {\n";
    s += "    \"clusters\": " + std::to_string(kClusters) + ",\n";
    s += "    \"nodes\": [";
    for (std::size_t i = 0; i < c.nodes.size(); ++i) {
        const Node& n = c.nodes[i];
        s += (i == 0) ? "\n      [" : ",\n      [";
        s += json::number(n.x) + ", " + json::number(n.y) + ", " + json::number(n.z) +
             ", " + std::to_string(n.cluster) + "]";
    }
    s += "\n    ],\n";
    s += "    \"edges\": [";
    for (std::size_t i = 0; i < c.edges.size(); ++i) {
        s += (i == 0) ? "\n      [" : ",\n      [";
        s += std::to_string(c.edges[i].first) + ", " + std::to_string(c.edges[i].second) + "]";
    }
    s += "\n    ],\n";
    s += "    \"shell\": [";
    for (std::size_t i = 0; i < c.shell.size(); ++i) {
        const auto& p = c.shell[i];
        s += (i == 0) ? "\n      [" : ",\n      [";
        s += json::number(p[0]) + ", " + json::number(p[1]) + ", " + json::number(p[2]) + "]";
    }
    s += "\n    ]\n";
    s += "  }\n";
    s += "}\n";
    return doc;
}

void warnUnknown(const std::vector<char32_t>& unknown) {
    if (unknown.empty()) return;
    std::set<char32_t> uniq(unknown.begin(), unknown.end());
    std::string list;
    for (char32_t cp : uniq) {
        char buf[16];
        std::snprintf(buf, sizeof buf, " U+%04X", static_cast<unsigned>(cp));
        list += buf;
        if (cp >= 0x21 && cp < 0x7F) {
            list += " '";
            list += static_cast<char>(cp);
            list += "'";
        }
    }
    std::fprintf(stderr, "asciigen: warning: unsupported character(s) rendered as '?':%s\n",
                 list.c_str());
}

bool writeAtomic(const std::string& path, const std::string& data, std::string& err) {
    const std::string tmp = path + ".tmp";
    {
        std::ofstream f(tmp, std::ios::binary | std::ios::trunc);
        if (!f) {
            err = "cannot open " + tmp + " for writing";
            return false;
        }
        f.write(data.data(), static_cast<std::streamsize>(data.size()));
        f.flush();
        if (!f) {
            err = "write failed for " + tmp;
            f.close();
            std::remove(tmp.c_str());
            return false;
        }
    }
    if (std::rename(tmp.c_str(), path.c_str()) != 0) {
        err = "cannot rename " + tmp + " to " + path;
        std::remove(tmp.c_str());
        return false;
    }
    return true;
}

// ---------------------------------------------------------------- self-test

struct Checker {
    int failures = 0;
    void check(bool ok, const std::string& what) {
        if (!ok) {
            ++failures;
            std::fprintf(stderr, "self-test FAIL: %s\n", what.c_str());
        }
    }
};

void checkBanner(Checker& t, const Banner& b, const std::string& label) {
    t.check(b.size() == static_cast<std::size_t>(kGlyphHeight), label + ": banner must have 7 rows");
    if (b.empty()) return;
    const std::size_t w = b[0].size();
    bool lastColInked = (w == 0);
    for (const auto& r : b) {
        t.check(r.size() == w, label + ": rows must have equal length");
        t.check(r.find_first_not_of("# ") == std::string::npos, label + ": only '#' and ' ' allowed");
        if (w > 0 && r.size() == w && r[w - 1] == '#') lastColInked = true;
    }
    t.check(lastColInked, label + ": trailing all-blank column");
}

void checkFont(Checker& t) {
    for (const auto& g : fontGlyphs()) {
        const std::string label = std::string("glyph '") + g.ch + "'";
        bool inked = false;
        for (int r = 0; r < kGlyphHeight; ++r) {
            const std::string row = g.rows[r] ? g.rows[r] : "";
            t.check(row.size() == static_cast<std::size_t>(kGlyphWidth), label + ": row width != 5");
            t.check(row.find_first_not_of("# ") == std::string::npos, label + ": bad pixel char");
            if (row.find('#') != std::string::npos) inked = true;
        }
        t.check(g.ch == ' ' ? !inked : inked, label + (g.ch == ' ' ? ": space must be blank" : ": glyph is empty"));
    }
    const std::string required =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .,-/&+':()!?";
    for (char ch : required) {
        t.check(findGlyph(ch) != nullptr, std::string("missing glyph '") + ch + "'");
    }
    std::vector<char32_t> unknown;
    checkBanner(t, renderBanner("the quick brown fox 0123456789 .,-/&+':()!?", &unknown), "all-glyph banner");
    t.check(unknown.empty(), "all-glyph banner reported unknown characters");
    unknown.clear();
    const Banner q = renderBanner("a\xC3\xA9#", &unknown);  // 'e-acute' and '#'
    t.check(unknown.size() == 2, "unsupported characters must be reported once each");
    checkBanner(t, q, "fallback banner");
    checkBanner(t, renderBanner("end. ", nullptr), "trailing punctuation/space banner");
}

void checkScene(Checker& t, const Constellation& c, int n, const std::string& label) {
    t.check(static_cast<int>(c.nodes.size()) == n, label + ": node count");
    std::vector<int> degree(static_cast<std::size_t>(n), 0);
    std::set<std::pair<int, int>> seen;
    int bridges = 0;
    std::vector<int> bridgeCount(static_cast<std::size_t>(n), 0);
    for (std::size_t k = 0; k < c.edges.size(); ++k) {
        const auto& e = c.edges[k];
        const bool inRange = e.first >= 0 && e.second < n;
        t.check(e.first < e.second, label + ": edge i<j");
        t.check(inRange, label + ": edge index range");
        t.check(seen.insert(e).second, label + ": duplicate edge");
        if (k > 0) t.check(c.edges[k - 1] < e, label + ": edges sorted");
        if (!inRange || e.first >= e.second) continue;
        ++degree[static_cast<std::size_t>(e.first)];
        ++degree[static_cast<std::size_t>(e.second)];
        if (c.nodes[static_cast<std::size_t>(e.first)].cluster !=
            c.nodes[static_cast<std::size_t>(e.second)].cluster) {
            ++bridges;
            ++bridgeCount[static_cast<std::size_t>(e.first)];
            ++bridgeCount[static_cast<std::size_t>(e.second)];
        }
    }
    t.check(bridges == kBridges, label + ": exactly 8 cross-cluster bridges (got " + std::to_string(bridges) + ")");
    for (int i = 0; i < n; ++i) {
        const int d = degree[static_cast<std::size_t>(i)];
        t.check(d >= kMinDegree && d <= kMaxDegree,
                label + ": node " + std::to_string(i) + " degree " + std::to_string(d));
        t.check(bridgeCount[static_cast<std::size_t>(i)] <= 1, label + ": node with >1 bridge");
        const Node& nd = c.nodes[static_cast<std::size_t>(i)];
        t.check(nd.cluster >= 0 && nd.cluster < kClusters, label + ": cluster id range");
        t.check(std::abs(nd.x) <= 1.0001 && std::abs(nd.y) <= 1.0001 && std::abs(nd.z) <= 1.0001,
                label + ": node coords within 1.0001");
    }
    for (const auto& p : c.shell) {
        t.check(std::abs(p[0]) <= 1.0001 && std::abs(p[1]) <= 1.0001 && std::abs(p[2]) <= 1.0001,
                label + ": shell coords within 1.0001");
    }
    t.check(!c.shell.empty(), label + ": shell empty");
}

int runSelfTest() {
    Checker t;
    checkFont(t);
    const std::uint64_t seeds[] = {1, 2026, 424242, 0xFFFFFFFFFFFFFFFFULL};
    const int counts[] = {kMinNodes, 150, kMaxNodes};
    for (std::uint64_t seed : seeds) {
        for (int n : counts) {
            const std::string label = "seed " + std::to_string(seed) + " nodes " + std::to_string(n);
            Options o;
            o.name = "Joshua D. Jones";
            o.tagline = "DevOps Engineer - CI/CD & K8s: (Zarf + GitLab)! Why? It's fun.";
            o.hasTagline = true;
            o.seed = seed;
            o.nodes = n;
            try {
                std::vector<char32_t> unknown;
                const Document a = buildDocument(o, &unknown);
                const Document b = buildDocument(o, &unknown);
                t.check(unknown.empty(), label + ": unexpected unknown characters");
                t.check(a.json == b.json, label + ": same seed must give identical JSON");
                bool ascii = true;
                for (unsigned char ch : a.json) {
                    if (ch >= 0x80) ascii = false;
                }
                t.check(ascii, label + ": JSON must be ASCII-only");
                for (std::size_t i = 0; i < a.banners.size(); ++i) {
                    checkBanner(t, a.banners[i], label + ": banner " + std::to_string(i));
                }
                t.check(a.banners.size() == 1 + 3 + 1 + 7, label + ": banner count");
                checkScene(t, a.scene, n, label);
                Options o2 = o;
                o2.seed = seed + 1;
                t.check(buildDocument(o2, nullptr).json != a.json, label + ": different seed must differ");
            } catch (const std::exception& e) {
                t.check(false, label + ": exception: " + e.what());
            }
        }
    }
    if (t.failures) {
        std::fprintf(stderr, "self-test: %d failure(s)\n", t.failures);
        return 1;
    }
    std::printf("self-test ok\n");
    return 0;
}

int usageError(const std::string& msg) {
    std::fprintf(stderr, "asciigen: %s\n", msg.c_str());
    printUsage(stderr);
    return 2;
}

}  // namespace

int main(int argc, char** argv) {
    Options o;
    bool selfTest = false;
    for (int i = 1; i < argc; ++i) {
        const std::string a = argv[i];
        if (a == "--help" || a == "-h") {
            printUsage(stdout);
            return 0;
        }
    }
    for (int i = 1; i < argc; ++i) {
        const std::string a = argv[i];
        if (a == "--self-test") {
            selfTest = true;
            continue;
        }
        const bool takesValue = a == "--name" || a == "--tagline" || a == "--headings" ||
                                a == "--seed" || a == "--out" || a == "--nodes";
        if (!takesValue) return usageError("unknown argument: " + a);
        if (i + 1 >= argc) return usageError("missing value for " + a);
        const std::string v = argv[++i];
        if (a == "--name") {
            o.name = v;
            o.hasName = true;
        } else if (a == "--tagline") {
            o.tagline = v;
            o.hasTagline = true;
        } else if (a == "--headings") {
            o.headings = v;
        } else if (a == "--out") {
            if (v.empty()) return usageError("empty value for --out");
            o.out = v;
            o.hasOut = true;
        } else if (a == "--seed") {
            if (!parseUint64(v, o.seed)) return usageError("bad number for --seed: " + v);
        } else {  // --nodes
            std::uint64_t n = 0;
            if (!parseUint64(v, n) || n < static_cast<std::uint64_t>(kMinNodes) ||
                n > static_cast<std::uint64_t>(kMaxNodes)) {
                return usageError("bad number for --nodes (allowed " + std::to_string(kMinNodes) +
                                  ".." + std::to_string(kMaxNodes) + "): " + v);
            }
            o.nodes = static_cast<int>(n);
        }
    }

    if (selfTest) return runSelfTest();
    if (!o.hasName || trim(o.name).empty()) return usageError("--name is required");

    std::vector<char32_t> unknown;
    Document doc;
    try {
        doc = buildDocument(o, &unknown);
    } catch (const std::exception& e) {
        std::fprintf(stderr, "asciigen: error: %s\n", e.what());
        return 1;
    }
    warnUnknown(unknown);

    if (o.hasOut) {
        std::string err;
        if (!writeAtomic(o.out, doc.json, err)) {
            std::fprintf(stderr, "asciigen: error: %s\n", err.c_str());
            return 1;
        }
    } else {
        if (std::fwrite(doc.json.data(), 1, doc.json.size(), stdout) != doc.json.size() ||
            std::fflush(stdout) != 0) {
            std::fprintf(stderr, "asciigen: error: failed writing to stdout\n");
            return 1;
        }
    }
    return 0;
}
