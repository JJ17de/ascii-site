// scene.hpp - deterministic 3D "network constellation" generation.
#pragma once

#include <array>
#include <cstdint>
#include <utility>
#include <vector>

namespace asciigen {

constexpr int kClusters = 3;
constexpr int kBridges = 8;
constexpr int kMinNodes = 20;
constexpr int kMaxNodes = 600;
constexpr int kMaxDegree = 6;
constexpr int kMinDegree = 2;

struct Node {
    double x, y, z;  // y is up
    int cluster;
};

struct Constellation {
    std::vector<Node> nodes;
    std::vector<std::pair<int, int>> edges;  // undirected, i<j, sorted, unique
    std::vector<std::array<double, 3>> shell;  // wireframe globe on unit sphere
};

// Builds the constellation for (seed, nodeCount). Throws std::runtime_error if
// the invariants cannot be satisfied (should not happen for 20..600 nodes).
Constellation buildConstellation(std::uint64_t seed, int nodeCount);

}  // namespace asciigen
