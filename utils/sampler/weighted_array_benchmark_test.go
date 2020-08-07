// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedArray1
func BenchmarkWeightedArray1(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 0, 1)
}

// BenchmarkWeightedArrayUniform10
func BenchmarkWeightedArrayUniform10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 0, 10)
}

// BenchmarkWeightedArrayUniform1000
func BenchmarkWeightedArrayUniform1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 0, 1000)
}

// BenchmarkWeightedArrayUniform100009
func BenchmarkWeightedArrayUniform100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 0, 100000)
}

// BenchmarkWeightedArrayLinear10
func BenchmarkWeightedArrayLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 10)
}

// BenchmarkWeightedArrayLinear1000
func BenchmarkWeightedArrayLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 1000)
}

// BenchmarkWeightedArrayLinear100000
func BenchmarkWeightedArrayLinear100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 1, 100000)
}

// BenchmarkWeightedArrayQuadratic10
func BenchmarkWeightedArrayQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 10)
}

// BenchmarkWeightedArrayQuadratic1000
func BenchmarkWeightedArrayQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 1000)
}

// BenchmarkWeightedArrayQuadratic100000
func BenchmarkWeightedArrayQuadratic100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 2, 100000)
}

// BenchmarkWeightedArrayCubic10
func BenchmarkWeightedArrayCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 10)
}

// BenchmarkWeightedArrayCubic1000
func BenchmarkWeightedArrayCubic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 1000)
}

// BenchmarkWeightedArrayCubic50000
func BenchmarkWeightedArrayCubic50000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedArray{}, 3, 50000)
}

// BenchmarkWeightedArraySingleton10
func BenchmarkWeightedArraySingleton10(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedArray{}, 10)
}

// BenchmarkWeightedArraySingleton1000
func BenchmarkWeightedArraySingleton1000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedArray{}, 1000)
}

// BenchmarkWeightedArraySingleton100000
func BenchmarkWeightedArraySingleton100000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedArray{}, 100000)
}
