// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedHeap1
func BenchmarkWeightedHeap1(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedHeap{}, 1)
}

// BenchmarkWeightedHeapUniform10
func BenchmarkWeightedHeapUniform10(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedHeap{}, 10)
}

// BenchmarkWeightedHeapUniform100
func BenchmarkWeightedHeapUniform100(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedHeap{}, 100)
}

// BenchmarkWeightedHeapUniform1000
func BenchmarkWeightedHeapUniform1000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedHeap{}, 1000)
}

// BenchmarkWeightedHeapUniform10000
func BenchmarkWeightedHeapUniform10000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedHeap{}, 10000)
}

// BenchmarkWeightedHeapUniform100000
func BenchmarkWeightedHeapUniform100000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedHeap{}, 100000)
}

// BenchmarkWeightedHeapLinear10
func BenchmarkWeightedHeapLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 1, 10)
}

// BenchmarkWeightedHeapLinear100
func BenchmarkWeightedHeapLinear100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 1, 100)
}

// BenchmarkWeightedHeapLinear1000
func BenchmarkWeightedHeapLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 1, 1000)
}

// BenchmarkWeightedHeapLinear10000
func BenchmarkWeightedHeapLinear10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 1, 10000)
}

// BenchmarkWeightedHeapLinear100000
func BenchmarkWeightedHeapLinear100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 1, 100000)
}

// BenchmarkWeightedHeapQuadratic10
func BenchmarkWeightedHeapQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 2, 10)
}

// BenchmarkWeightedHeapQuadratic100
func BenchmarkWeightedHeapQuadratic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 2, 100)
}

// BenchmarkWeightedHeapQuadratic1000
func BenchmarkWeightedHeapQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 2, 1000)
}

// BenchmarkWeightedHeapQuadratic10000
func BenchmarkWeightedHeapQuadratic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 2, 10000)
}

// BenchmarkWeightedHeapQuadratic100000
func BenchmarkWeightedHeapQuadratic100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 2, 100000)
}

// BenchmarkWeightedHeapCubic10
func BenchmarkWeightedHeapCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 3, 10)
}

// BenchmarkWeightedHeapCubic100
func BenchmarkWeightedHeapCubic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 3, 100)
}

// BenchmarkWeightedHeapCubic1000
func BenchmarkWeightedHeapCubic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 3, 1000)
}

// BenchmarkWeightedHeapCubic10000
func BenchmarkWeightedHeapCubic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 3, 10000)
}

// BenchmarkWeightedHeapCubic50000
func BenchmarkWeightedHeapCubic50000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedHeap{}, 3, 50000)
}

// BenchmarkWeightedHeapSingleton10
func BenchmarkWeightedHeapSingleton10(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedHeap{}, 10)
}

// BenchmarkWeightedHeapSingleton100
func BenchmarkWeightedHeapSingleton100(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedHeap{}, 100)
}

// BenchmarkWeightedHeapSingleton1000
func BenchmarkWeightedHeapSingleton1000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedHeap{}, 1000)
}

// BenchmarkWeightedHeapSingleton10000
func BenchmarkWeightedHeapSingleton10000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedHeap{}, 10000)
}

// BenchmarkWeightedHeapSingleton100000
func BenchmarkWeightedHeapSingleton100000(b *testing.B) {
	WeightedSingletonBenchmark(b, &weightedHeap{}, 100000)
}
