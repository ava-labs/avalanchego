// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedUniform1
func BenchmarkWeightedUniform1(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 0, 1)
}

// BenchmarkWeightedUniformUniform10
func BenchmarkWeightedUniformUniform10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 0, 10)
}

// BenchmarkWeightedUniformUniform1000
func BenchmarkWeightedUniformUniform1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 0, 1000)
}

// BenchmarkWeightedUniformUniform100000
func BenchmarkWeightedUniformUniform100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 0, 100000)
}

// BenchmarkWeightedUniformLinear10
func BenchmarkWeightedUniformLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 1, 10)
}

// BenchmarkWeightedUniformLinear1000
func BenchmarkWeightedUniformLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 1, 1000)
}

// BenchmarkWeightedUniformQuadratic10
func BenchmarkWeightedUniformQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 2, 10)
}

// BenchmarkWeightedUniformQuadratic1000
func BenchmarkWeightedUniformQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 2, 1000)
}

// BenchmarkWeightedUniformCubic10
func BenchmarkWeightedUniformCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedUniform{}, 3, 10)
}
