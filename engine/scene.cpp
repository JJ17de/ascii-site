// scene.cpp - constellation nodes, edges and wireframe shell.
//
// Determinism notes: only +, -, *, / and sqrt are used on doubles (all
// correctly rounded by IEEE 754), trig comes from the polynomial below instead
// of the platform libm, and FMA contraction is disabled for clang (rng.hpp),
// so the same seed yields the same bits on Linux/x86-64 and macOS/arm64.
#if defined(__clang__)
#pragma STDC FP_CONTRACT OFF
#endif

#include "scene.hpp"

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <set>
#include <stdexcept>

#include "rng.hpp"

namespace asciigen {

namespace {

constexpr double kPi = 3.14159265358979323846;
constexpr double kTwoPi = 2.0 * kPi;
constexpr double kShellStep = 0.035;
constexpr int kShellRings = 11;
constexpr int kShellMeridians = 16;
constexpr double kBridgeMinSeparation = 0.35;

// Deterministic sin/cos: Cody-Waite reduction by pi/2, Taylor polynomials on
// [-pi/4, pi/4]. Accurate to ~1e-16 for |x| < 1e5, far beyond 4 decimals.
void detSinCos(double x, double& s, double& c) {
    const double invPio2 = 6.36619772367581382433e-01;
    const double pio2Hi = 1.57079632673412561417e+00;  // first 33 bits of pi/2
    const double pio2Lo = 6.07710050650619224932e-11;  // pi/2 - pio2Hi
    const double kf = std::floor(x * invPio2 + 0.5);
    const double r = (x - kf * pio2Hi) - kf * pio2Lo;
    const double r2 = r * r;
    const double sr =
        r * (1.0 + r2 * (-1.0 / 6.0 + r2 * (1.0 / 120.0 + r2 * (-1.0 / 5040.0 +
             r2 * (1.0 / 362880.0 + r2 * (-1.0 / 39916800.0 +
             r2 * (1.0 / 6227020800.0 + r2 * (-1.0 / 1307674368000.0))))))));
    const double cr =
        1.0 + r2 * (-0.5 + r2 * (1.0 / 24.0 + r2 * (-1.0 / 720.0 +
              r2 * (1.0 / 40320.0 + r2 * (-1.0 / 3628800.0 +
              r2 * (1.0 / 479001600.0 + r2 * (-1.0 / 87178291200.0 +
              r2 * (1.0 / 20922789888000.0))))))));
    const long long k = static_cast<long long>(kf);
    switch (((k % 4) + 4) % 4) {
        case 0: s = sr;  c = cr;  break;
        case 1: s = cr;  c = -sr; break;
        case 2: s = -sr; c = -cr; break;
        default: s = -cr; c = sr; break;
    }
}

double dist2(const Node& a, const Node& b) {
    const double dx = a.x - b.x;
    const double dy = a.y - b.y;
    const double dz = a.z - b.z;
    return dx * dx + dy * dy + dz * dz;
}

using Edge = std::pair<int, int>;

Edge makeEdge(int a, int b) { return a < b ? Edge(a, b) : Edge(b, a); }

void placeNodes(SplitMix64& rng, int n, std::vector<Node>& nodes) {
    const double golden = kPi * (3.0 - std::sqrt(5.0));
    const double spacing = std::sqrt(4.0 * kPi / static_cast<double>(n));
    const double jitter = 0.18 * spacing;
    nodes.clear();
    nodes.reserve(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const double y = 1.0 - 2.0 * (static_cast<double>(i) + 0.5) / static_cast<double>(n);
        const double ring = std::sqrt(std::max(0.0, 1.0 - y * y));
        double s = 0.0, c = 0.0;
        detSinCos(golden * static_cast<double>(i), s, c);
        // Draw jitter in a fixed order (never several rng calls in one expression).
        const double jx = rng.uniform(-jitter, jitter);
        const double jy = rng.uniform(-jitter, jitter);
        const double jz = rng.uniform(-jitter, jitter);
        double dx = c * ring + jx;
        double dy = y + jy;
        double dz = s * ring + jz;
        const double len = std::sqrt(dx * dx + dy * dy + dz * dz);
        dx /= len;
        dy /= len;
        dz /= len;
        const double radius = rng.uniform(0.72, 1.0);
        nodes.push_back(Node{dx * radius, dy * radius, dz * radius, 0});
    }
}

// Clusters = three 120-degree longitude sectors around the y axis, rotated by
// a seeded offset. Sector membership uses cross products only (no atan2).
void assignClusters(double offset, std::vector<Node>& nodes) {
    double bx[kClusters + 1];
    double bz[kClusters + 1];
    for (int k = 0; k <= kClusters; ++k) {
        double s = 0.0, c = 0.0;
        detSinCos(offset + kTwoPi * static_cast<double>(k) / kClusters, s, c);
        bx[k] = c;
        bz[k] = s;
    }
    for (auto& nd : nodes) {
        nd.cluster = 0;
        for (int k = 0; k < kClusters; ++k) {
            const double afterStart = bx[k] * nd.z - bz[k] * nd.x;
            const double beforeEnd = nd.x * bz[k + 1] - nd.z * bx[k + 1];
            if (afterStart >= 0.0 && beforeEnd > 0.0) {
                nd.cluster = k;
                break;
            }
        }
    }
}

void buildIntraClusterEdges(const std::vector<Node>& nodes, std::set<Edge>& edges,
                            std::vector<int>& degree) {
    const int n = static_cast<int>(nodes.size());
    std::vector<std::vector<int>> ranked(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        auto& cand = ranked[static_cast<std::size_t>(i)];
        for (int j = 0; j < n; ++j) {
            if (j != i && nodes[static_cast<std::size_t>(j)].cluster ==
                              nodes[static_cast<std::size_t>(i)].cluster) {
                cand.push_back(j);
            }
        }
        const Node& ni = nodes[static_cast<std::size_t>(i)];
        std::sort(cand.begin(), cand.end(), [&](int a, int b) {
            const double da = dist2(ni, nodes[static_cast<std::size_t>(a)]);
            const double db = dist2(ni, nodes[static_cast<std::size_t>(b)]);
            return da < db || (da == db && a < b);
        });
    }

    auto tryLink = [&](int i, int j) {
        const Edge e = makeEdge(i, j);
        if (edges.count(e)) return;
        if (degree[static_cast<std::size_t>(i)] >= kMaxDegree ||
            degree[static_cast<std::size_t>(j)] >= kMaxDegree) {
            return;  // degree cap: skip the extra link
        }
        edges.insert(e);
        ++degree[static_cast<std::size_t>(i)];
        ++degree[static_cast<std::size_t>(j)];
    };

    // Pass 1: each node links to its 2 nearest same-cluster neighbours.
    for (int i = 0; i < n; ++i) {
        const auto& cand = ranked[static_cast<std::size_t>(i)];
        for (std::size_t k = 0; k < cand.size() && k < 2; ++k) tryLink(i, cand[k]);
    }
    // Pass 2: guarantee degree >= 2 (only needed if the cap blocked a link).
    for (int i = 0; i < n; ++i) {
        const auto& cand = ranked[static_cast<std::size_t>(i)];
        for (std::size_t k = 2; k < cand.size() && degree[static_cast<std::size_t>(i)] < kMinDegree; ++k) {
            tryLink(i, cand[k]);
        }
    }
}

void addBridges(const std::vector<Node>& nodes, std::set<Edge>& edges, std::vector<int>& degree) {
    const int n = static_cast<int>(nodes.size());
    const int pairs[kClusters][2] = {{0, 1}, {1, 2}, {0, 2}};
    std::vector<bool> bridged(static_cast<std::size_t>(n), false);
    std::vector<std::array<double, 3>> mids;

    // Closest eligible pair between clusters ca and cb (ca<0: any two clusters).
    auto findBest = [&](int ca, int cb, bool spread, int& bi, int& bj) {
        double best = 0.0;
        bi = bj = -1;
        for (int i = 0; i < n; ++i) {
            const Node& a = nodes[static_cast<std::size_t>(i)];
            if (bridged[static_cast<std::size_t>(i)] || degree[static_cast<std::size_t>(i)] >= kMaxDegree) continue;
            for (int j = i + 1; j < n; ++j) {
                const Node& b = nodes[static_cast<std::size_t>(j)];
                if (a.cluster == b.cluster) continue;
                if (ca >= 0 && !((a.cluster == ca && b.cluster == cb) ||
                                 (a.cluster == cb && b.cluster == ca))) {
                    continue;
                }
                if (bridged[static_cast<std::size_t>(j)] || degree[static_cast<std::size_t>(j)] >= kMaxDegree) continue;
                if (edges.count(Edge(i, j))) continue;
                const double d = dist2(a, b);
                if (bi >= 0 && !(d < best)) continue;
                if (spread) {
                    const double mx = 0.5 * (a.x + b.x);
                    const double my = 0.5 * (a.y + b.y);
                    const double mz = 0.5 * (a.z + b.z);
                    bool tooClose = false;
                    for (const auto& m : mids) {
                        const double ex = mx - m[0], ey = my - m[1], ez = mz - m[2];
                        if (ex * ex + ey * ey + ez * ez < kBridgeMinSeparation * kBridgeMinSeparation) {
                            tooClose = true;
                            break;
                        }
                    }
                    if (tooClose) continue;
                }
                best = d;
                bi = i;
                bj = j;
            }
        }
        return bi >= 0;
    };

    for (int b = 0; b < kBridges; ++b) {
        const int ca = pairs[b % kClusters][0];
        const int cb = pairs[b % kClusters][1];
        int i = -1, j = -1;
        if (!findBest(ca, cb, true, i, j) && !findBest(ca, cb, false, i, j) &&
            !findBest(-1, -1, true, i, j) && !findBest(-1, -1, false, i, j)) {
            throw std::runtime_error("could not place all bridge edges");
        }
        edges.insert(Edge(i, j));
        ++degree[static_cast<std::size_t>(i)];
        ++degree[static_cast<std::size_t>(j)];
        bridged[static_cast<std::size_t>(i)] = true;
        bridged[static_cast<std::size_t>(j)] = true;
        const Node& a = nodes[static_cast<std::size_t>(i)];
        const Node& c = nodes[static_cast<std::size_t>(j)];
        mids.push_back({0.5 * (a.x + c.x), 0.5 * (a.y + c.y), 0.5 * (a.z + c.z)});
    }
}

void buildShell(std::vector<std::array<double, 3>>& shell) {
    shell.clear();
    // Latitude rings, poles excluded: latitudes -75..75 in 15-degree steps.
    for (int k = 1; k <= kShellRings; ++k) {
        const double lat = -kPi / 2.0 + kPi * static_cast<double>(k) / (kShellRings + 1);
        double sl = 0.0, cl = 0.0;
        detSinCos(lat, sl, cl);
        const int count = std::max(3, static_cast<int>(std::floor(kTwoPi * cl / kShellStep + 0.5)));
        for (int m = 0; m < count; ++m) {
            double so = 0.0, co = 0.0;
            detSinCos(kTwoPi * static_cast<double>(m) / count, so, co);
            shell.push_back({cl * co, sl, cl * so});
        }
    }
    // Meridians: half great circles from south pole to north pole (inclusive).
    const int segs = static_cast<int>(std::floor(kPi / kShellStep + 0.5));
    for (int m = 0; m < kShellMeridians; ++m) {
        double so = 0.0, co = 0.0;
        detSinCos(kTwoPi * static_cast<double>(m) / kShellMeridians, so, co);
        for (int t = 0; t <= segs; ++t) {
            const double lat = -kPi / 2.0 + kPi * static_cast<double>(t) / segs;
            double sl = 0.0, cl = 0.0;
            detSinCos(lat, sl, cl);
            shell.push_back({cl * co, sl, cl * so});
        }
    }
}

}  // namespace

Constellation buildConstellation(std::uint64_t seed, int nodeCount) {
    if (nodeCount < kMinNodes || nodeCount > kMaxNodes) {
        throw std::runtime_error("node count out of range");
    }
    SplitMix64 rng(seed);
    Constellation out;
    const double offset = rng.uniform(0.0, kTwoPi);
    placeNodes(rng, nodeCount, out.nodes);
    assignClusters(offset, out.nodes);

    std::set<Edge> edges;
    std::vector<int> degree(static_cast<std::size_t>(nodeCount), 0);
    buildIntraClusterEdges(out.nodes, edges, degree);
    addBridges(out.nodes, edges, degree);
    out.edges.assign(edges.begin(), edges.end());  // std::set keeps them sorted

    buildShell(out.shell);
    return out;
}

}  // namespace asciigen
