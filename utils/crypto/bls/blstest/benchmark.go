// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blstest

var (
	BenchmarkSizes = []int{
		1,
		2,
		4,
		8,
		16,
		32,
		64,
		128,
		256,
		512,
		1024,
		2048,
		4096,
		8192,
		16384,
		32768,
	}
	BiggestBenchmarkSize = BenchmarkSizes[len(BenchmarkSizes)-1]
)
