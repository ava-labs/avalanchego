// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedMixed1
func BenchmarkWeightedMixed1(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1)
}

// BenchmarkWeightedMixedUniform10
func BenchmarkWeightedMixedUniform10(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 10)
}

// BenchmarkWeightedMixedUniform100
func BenchmarkWeightedMixedUniform100(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 100)
}

// BenchmarkWeightedMixedUniform1000
func BenchmarkWeightedMixedUniform1000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1000)
}

// BenchmarkWeightedMixedUniform10000
func BenchmarkWeightedMixedUniform10000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 10000)
}

// BenchmarkWeightedMixedUniform100000
func BenchmarkWeightedMixedUniform100000(b *testing.B) {
	WeightedUniformBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 100000)
}

// BenchmarkWeightedMixedLinear10
func BenchmarkWeightedMixedLinear10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1, 10)
}

// BenchmarkWeightedMixedLinear100
func BenchmarkWeightedMixedLinear100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1, 100)
}

// BenchmarkWeightedMixedLinear1000
func BenchmarkWeightedMixedLinear1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1, 1000)
}

// BenchmarkWeightedMixedLinear10000
func BenchmarkWeightedMixedLinear10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1, 10000)
}

// BenchmarkWeightedMixedLinear100000
func BenchmarkWeightedMixedLinear100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 1, 100000)
}

// BenchmarkWeightedMixedQuadratic10
func BenchmarkWeightedMixedQuadratic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 2, 10)
}

// BenchmarkWeightedMixedQuadratic100
func BenchmarkWeightedMixedQuadratic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 2, 100)
}

// BenchmarkWeightedMixedQuadratic1000
func BenchmarkWeightedMixedQuadratic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 2, 1000)
}

// BenchmarkWeightedMixedQuadratic10000
func BenchmarkWeightedMixedQuadratic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 2, 10000)
}

// BenchmarkWeightedMixedQuadratic100000
func BenchmarkWeightedMixedQuadratic100000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 2, 100000)
}

// BenchmarkWeightedMixedCubic10
func BenchmarkWeightedMixedCubic10(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 3, 10)
}

// BenchmarkWeightedMixedCubic100
func BenchmarkWeightedMixedCubic100(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 3, 100)
}

// BenchmarkWeightedMixedCubic1000
func BenchmarkWeightedMixedCubic1000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 3, 1000)
}

// BenchmarkWeightedMixedCubic10000
func BenchmarkWeightedMixedCubic10000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 3, 10000)
}

// BenchmarkWeightedMixedCubic50000
func BenchmarkWeightedMixedCubic50000(b *testing.B) {
	WeightedPowBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 3, 50000)
}

// BenchmarkWeightedMixedExponential10
func BenchmarkWeightedMixedExponential10(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 10)
}

// BenchmarkWeightedMixedExponential20
func BenchmarkWeightedMixedExponential20(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 20)
}

// BenchmarkWeightedMixedExponential40
func BenchmarkWeightedMixedExponential40(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 40)
}

// BenchmarkWeightedMixedExponential60
func BenchmarkWeightedMixedExponential60(b *testing.B) {
	WeightedExponentialBenchmark(b, &weightedMixed{
		weighteds: []Weighted{&weightedArray{}, &weightedHeap{}},
	}, 60)
}
