// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedArray1
func BenchmarkWeightedArray1(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedArray{}, 1)
}

// BenchmarkWeightedArrayUniform10
func BenchmarkWeightedArrayUniform10(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedArray{}, 10)
}

// BenchmarkWeightedArrayUniform100
func BenchmarkWeightedArrayUniform100(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedArray{}, 100)
}

// BenchmarkWeightedArrayUniform1000
func BenchmarkWeightedArrayUniform1000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedArray{}, 1000)
}

// BenchmarkWeightedArrayUniform10000
func BenchmarkWeightedArrayUniform10000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedArray{}, 10000)
}

// BenchmarkWeightedArrayUniform100009
func BenchmarkWeightedArrayUniform100000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedArray{}, 100000)
}

// BenchmarkWeightedArrayLinear10
func BenchmarkWeightedArrayLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 10)
}

// BenchmarkWeightedArrayLinear100
func BenchmarkWeightedArrayLinear100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 100)
}

// BenchmarkWeightedArrayLinear1000
func BenchmarkWeightedArrayLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 1000)
}

// BenchmarkWeightedArrayLinear10000
func BenchmarkWeightedArrayLinear10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 10000)
}

// BenchmarkWeightedArrayLinear100000
func BenchmarkWeightedArrayLinear100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 100000)
}

// BenchmarkWeightedArrayQuadratic10
func BenchmarkWeightedArrayQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 10)
}

// BenchmarkWeightedArrayQuadratic100
func BenchmarkWeightedArrayQuadratic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 100)
}

// BenchmarkWeightedArrayQuadratic1000
func BenchmarkWeightedArrayQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 1000)
}

// BenchmarkWeightedArrayQuadratic10000
func BenchmarkWeightedArrayQuadratic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 10000)
}

// BenchmarkWeightedArrayQuadratic100000
func BenchmarkWeightedArrayQuadratic100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 100000)
}

// BenchmarkWeightedArrayCubic10
func BenchmarkWeightedArrayCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 10)
}

// BenchmarkWeightedArrayCubic100
func BenchmarkWeightedArrayCubic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 100)
}

// BenchmarkWeightedArrayCubic1000
func BenchmarkWeightedArrayCubic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 1000)
}

// BenchmarkWeightedArrayCubic10000
func BenchmarkWeightedArrayCubic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 10000)
}

// BenchmarkWeightedArrayCubic50000
func BenchmarkWeightedArrayCubic50000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 50000)
}

// BenchmarkWeightedArrayExponential10
func BenchmarkWeightedArrayExponential10(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedArray{}, 10)
}

// BenchmarkWeightedArrayExponential20
func BenchmarkWeightedArrayExponential20(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedArray{}, 20)
}

// BenchmarkWeightedArrayExponential40
func BenchmarkWeightedArrayExponential40(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedArray{}, 40)
}

// BenchmarkWeightedArrayExponential60
func BenchmarkWeightedArrayExponential60(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedArray{}, 60)
}
