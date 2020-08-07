// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkUniformReplacer1
func BenchmarkUniformReplacer1(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 1)
}

// BenchmarkUniformReplacer10
func BenchmarkUniformReplacer10(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 10)
}

// BenchmarkUniformReplacer1000
func BenchmarkUniformReplacer1000(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 1000)
}

// BenchmarkUniformReplacer100000
func BenchmarkUniformReplacer100000(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 100000)
}
