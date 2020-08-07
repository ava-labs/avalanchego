// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

// BenchmarkWeightedWithoutReplacementGeneric1
func BenchmarkWeightedWithoutReplacementGeneric1(b *testing.B) {
	WeightedWithoutReplacementPowBenchmark(
		b,
		&weightedWithoutReplacementGeneric{
			u: &uniformReplacer{},
			w: &weightedHeap{},
		},
		0,
		1000000,
		1,
	)
}

// BenchmarkWeightedWithoutReplacementGenericUniform5
func BenchmarkWeightedWithoutReplacementGenericUniform5(b *testing.B) {
	WeightedWithoutReplacementPowBenchmark(
		b,
		&weightedWithoutReplacementGeneric{
			u: &uniformReplacer{},
			w: &weightedHeap{},
		},
		0,
		1000000,
		5,
	)
}

// BenchmarkWeightedWithoutReplacementGenericUniform25
func BenchmarkWeightedWithoutReplacementGenericUniform25(b *testing.B) {
	WeightedWithoutReplacementPowBenchmark(
		b,
		&weightedWithoutReplacementGeneric{
			u: &uniformReplacer{},
			w: &weightedHeap{},
		},
		0,
		1000000,
		25,
	)
}

// BenchmarkWeightedWithoutReplacementGenericUniform50
func BenchmarkWeightedWithoutReplacementGenericUniform50(b *testing.B) {
	WeightedWithoutReplacementPowBenchmark(
		b,
		&weightedWithoutReplacementGeneric{
			u: &uniformReplacer{},
			w: &weightedHeap{},
		},
		0,
		1000000,
		50,
	)
}

// BenchmarkWeightedWithoutReplacementGenericUniform75
func BenchmarkWeightedWithoutReplacementGenericUniform75(b *testing.B) {
	WeightedWithoutReplacementPowBenchmark(
		b,
		&weightedWithoutReplacementGeneric{
			u: &uniformReplacer{},
			w: &weightedHeap{},
		},
		0,
		1000000,
		75,
	)
}

// BenchmarkWeightedWithoutReplacementGenericUniform100
func BenchmarkWeightedWithoutReplacementGenericUniform100(b *testing.B) {
	WeightedWithoutReplacementPowBenchmark(
		b,
		&weightedWithoutReplacementGeneric{
			u: &uniformReplacer{},
			w: &weightedHeap{},
		},
		0,
		1000000,
		100,
	)
}
