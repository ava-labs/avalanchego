// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"math/rand"
	"testing"
)

func TestZipfDist(t *testing.T) {
	dbSize := uint64(1000000)
	chunkSize := 10000
	for _, sVal := range []float64{1.01, 1.1, 1.2, 1.3, 1.4} {
		for _, vVal := range []float64{1.0} {
			zipf := rand.NewZipf(rand.New(rand.NewSource(0)), sVal, vVal, dbSize)
			samples := make(map[uint64]uint64)
			for i := 0; i < chunkSize; i++ {
				samples[zipf.Uint64()]++
			}
			t.Logf("number of unique keys for sVal = %v and vVal = %v is %d when sampling %d out of %d\n", sVal, vVal, len(samples), chunkSize, dbSize)
		}
	}
}
