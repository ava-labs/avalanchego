// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedLinear1
func BenchmarkWeightedLinear1(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 1)
}

// BenchmarkWeightedLinearUniform10
func BenchmarkWeightedLinearUniform10(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 10)
}

// BenchmarkWeightedLinearUniform1000
func BenchmarkWeightedLinearUniform1000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 1000)
}

// BenchmarkWeightedLinearUniform100000
func BenchmarkWeightedLinearUniform100000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 100000)
}

// BenchmarkWeightedLinearLinear10
func BenchmarkWeightedLinearLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 10)
}

// BenchmarkWeightedLinearLinear1000
func BenchmarkWeightedLinearLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 1000)
}

// BenchmarkWeightedLinearLinear100000
func BenchmarkWeightedLinearLinear100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 100000)
}

// BenchmarkWeightedLinearQuadratic10
func BenchmarkWeightedLinearQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 10)
}

// BenchmarkWeightedLinearQuadratic1000
func BenchmarkWeightedLinearQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 1000)
}

// BenchmarkWeightedLinearQuadratic100000
func BenchmarkWeightedLinearQuadratic100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 100000)
}

// BenchmarkWeightedLinearCubic10
func BenchmarkWeightedLinearCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 10)
}

// BenchmarkWeightedLinearCubic1000
func BenchmarkWeightedLinearCubic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 1000)
}

// BenchmarkWeightedLinearCubic50000
func BenchmarkWeightedLinearCubic50000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 50000)
}

// BenchmarkWeightedLinearSingleton10
func BenchmarkWeightedLinearSingleton10(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedLinear{}, 10)
}

// BenchmarkWeightedLinearSingleton1000
func BenchmarkWeightedLinearSingleton1000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedLinear{}, 1000)
}

// BenchmarkWeightedLinearSingleton100000
func BenchmarkWeightedLinearSingleton100000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedLinear{}, 100000)
}
