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

// BenchmarkUniformReplacer5
func BenchmarkUniformReplacer5(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 5)
}

// BenchmarkUniformReplacer25
func BenchmarkUniformReplacer25(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 25)
}

// BenchmarkUniformReplacer50
func BenchmarkUniformReplacer50(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 50)
}

// BenchmarkUniformReplacer75
func BenchmarkUniformReplacer75(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 75)
}

// BenchmarkUniformReplacer100
func BenchmarkUniformReplacer100(b *testing.B) {
	UniformBenchmark(b, &uniformReplacer{}, 1000000, 100)
}
