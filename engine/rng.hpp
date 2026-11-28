// rng.hpp - deterministic, platform-independent PRNG (splitmix64).
//
// Never use std::random_device or <random> distributions here: their output
// differs between standard libraries, which would break byte-identical builds.
#pragma once

#if defined(__clang__)
// Forbid fused multiply-add contraction so arithmetic rounds identically on
// x86-64 and arm64 (Apple clang contracts a*b+c by default on arm64).
#pragma STDC FP_CONTRACT OFF
#endif

#include <cstdint>

namespace asciigen {

class SplitMix64 {
   public:
    explicit SplitMix64(std::uint64_t seed) : state_(seed) {}

    std::uint64_t next() {
        std::uint64_t z = (state_ += 0x9E3779B97F4A7C15ULL);
        z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9ULL;
        z = (z ^ (z >> 27)) * 0x94D049BB133111EBULL;
        return z ^ (z >> 31);
    }

    // Uniform double in [0, 1) built from the top 53 bits (exact conversion).
    double uniform() { return static_cast<double>(next() >> 11) * (1.0 / 9007199254740992.0); }

    // Uniform double in [lo, hi).
    double uniform(double lo, double hi) {
        const double u = uniform();
        return lo + (hi - lo) * u;
    }

   private:
    std::uint64_t state_;
};

}  // namespace asciigen
