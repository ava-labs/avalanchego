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

// BenchmarkWeightedLinearUniform100
func BenchmarkWeightedLinearUniform100(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 100)
}

// BenchmarkWeightedLinearUniform1000
func BenchmarkWeightedLinearUniform1000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 1000)
}

// BenchmarkWeightedLinearUniform10000
func BenchmarkWeightedLinearUniform10000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 10000)
}

// BenchmarkWeightedLinearUniform100000
func BenchmarkWeightedLinearUniform100000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedLinear{}, 100000)
}

// BenchmarkWeightedLinearLinear10
func BenchmarkWeightedLinearLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 10)
}

// BenchmarkWeightedLinearLinear100
func BenchmarkWeightedLinearLinear100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 100)
}

// BenchmarkWeightedLinearLinear1000
func BenchmarkWeightedLinearLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 1000)
}

// BenchmarkWeightedLinearLinear10000
func BenchmarkWeightedLinearLinear10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 10000)
}

// BenchmarkWeightedLinearLinear100000
func BenchmarkWeightedLinearLinear100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 1, 100000)
}

// BenchmarkWeightedLinearQuadratic10
func BenchmarkWeightedLinearQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 10)
}

// BenchmarkWeightedLinearQuadratic100
func BenchmarkWeightedLinearQuadratic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 100)
}

// BenchmarkWeightedLinearQuadratic1000
func BenchmarkWeightedLinearQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 1000)
}

// BenchmarkWeightedLinearQuadratic10000
func BenchmarkWeightedLinearQuadratic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 10000)
}

// BenchmarkWeightedLinearQuadratic100000
func BenchmarkWeightedLinearQuadratic100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 2, 100000)
}

// BenchmarkWeightedLinearCubic10
func BenchmarkWeightedLinearCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 10)
}

// BenchmarkWeightedLinearCubic100
func BenchmarkWeightedLinearCubic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 100)
}

// BenchmarkWeightedLinearCubic1000
func BenchmarkWeightedLinearCubic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 1000)
}

// BenchmarkWeightedLinearCubic10000
func BenchmarkWeightedLinearCubic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 10000)
}

// BenchmarkWeightedLinearCubic50000
func BenchmarkWeightedLinearCubic50000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedLinear{}, 3, 50000)
}

// BenchmarkWeightedLinearExponential10
func BenchmarkWeightedLinearExponential10(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedLinear{}, 10)
}

// BenchmarkWeightedLinearExponential20
func BenchmarkWeightedLinearExponential20(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedLinear{}, 20)
}

// BenchmarkWeightedLinearExponential40
func BenchmarkWeightedLinearExponential40(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedLinear{}, 40)
}

// BenchmarkWeightedLinearExponential60
func BenchmarkWeightedLinearExponential60(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedLinear{}, 60)
}
