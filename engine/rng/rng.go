// Package rng provides the deterministic random source the simulation runs on.
//
// Every random draw in the game comes from here, seeded from the save's master
// seed. That makes a save reproducible: replaying the same season with the same
// decisions produces the same results, which matters for debugging and keeps
// save-scumming from silently changing history.
package rng

import "math"

// R is a xoshiro256** generator. It is small, fast and has no allocation, which
// matters when a full season fires millions of draws.
type R struct{ s [4]uint64 }

// New seeds a generator using SplitMix64, so even a low-entropy seed such as 1
// produces a well-distributed state.
func New(seed uint64) *R {
	r := &R{}
	z := seed
	for i := 0; i < 4; i++ {
		z += 0x9E3779B97F4A7C15
		x := z
		x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
		x = (x ^ (x >> 27)) * 0x94D049BB133111EB
		r.s[i] = x ^ (x >> 31)
	}
	return r
}

// Derive returns an independent generator for a sub-simulation, so that
// simulating a match does not disturb the caller's stream. Two calls with the
// same key always yield the same generator.
func Derive(seed, key uint64) *R {
	return New(seed ^ (key * 0x9E3779B97F4A7C15))
}

func rotl(x uint64, k int) uint64 { return (x << k) | (x >> (64 - k)) }

// Uint64 returns the next raw 64-bit value.
func (r *R) Uint64() uint64 {
	res := rotl(r.s[1]*5, 7) * 9
	t := r.s[1] << 17
	r.s[2] ^= r.s[0]
	r.s[3] ^= r.s[1]
	r.s[1] ^= r.s[2]
	r.s[0] ^= r.s[3]
	r.s[2] ^= t
	r.s[3] = rotl(r.s[3], 45)
	return res
}

// Float returns a value in [0,1).
func (r *R) Float() float64 { return float64(r.Uint64()>>11) / (1 << 53) }

// Intn returns a value in [0,n). It returns 0 for n <= 0.
func (r *R) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Uint64() % uint64(n))
}

// Range returns a value in [lo,hi].
func (r *R) Range(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + r.Intn(hi-lo+1)
}

// Chance reports whether an event with probability p occurred.
func (r *R) Chance(p float64) bool { return r.Float() < p }

// Norm returns a normally distributed value with the given mean and standard
// deviation, using the Box-Muller transform.
func (r *R) Norm(mean, sd float64) float64 {
	u1 := r.Float()
	if u1 < 1e-12 {
		u1 = 1e-12
	}
	u2 := r.Float()
	return mean + sd*math.Sqrt(-2*math.Log(u1))*math.Cos(2*math.Pi*u2)
}

// Pick returns a random element index weighted by the supplied weights.
// Negative weights are treated as zero. Returns -1 if every weight is zero.
func (r *R) Pick(weights []float64) int {
	var total float64
	for _, w := range weights {
		if w > 0 {
			total += w
		}
	}
	if total <= 0 {
		return -1
	}
	x := r.Float() * total
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		x -= w
		if x <= 0 {
			return i
		}
	}
	// Floating point drift: fall back to the last positive weight.
	for i := len(weights) - 1; i >= 0; i-- {
		if weights[i] > 0 {
			return i
		}
	}
	return -1
}

// Shuffle randomises a slice in place via the supplied swap function.
func (r *R) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, r.Intn(i+1))
	}
}

// State exposes the generator's internal state so a save file can resume the
// exact random stream it was on.
func (r *R) State() [4]uint64 { return r.s }

// SetState restores a previously saved state.
func (r *R) SetState(s [4]uint64) { r.s = s }
