// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func TestWeighted(t *testing.T) {
	rand.Seed(0)

	counts := [countSize]int{}
	for i := 0; i < iterations; i++ {
		s := &Weighted{Weights: []uint64{0, 1, 2, 3, 4}}
		subset := Subset(s, 1)
		for _, j := range subset {
			counts[j]++
		}
		if len(subset) != 1 {
			t.Fatalf("Incorrect size")
		}
	}

	for i := 0; i < countSize; i++ {
		expected := float64(i) * iterations / 10
		if math.Abs(float64(counts[i])-expected) > threshold {
			t.Fatalf("Index seems biased: %s i=%d e=%f", fmt.Sprint(counts), i, expected)
		}
	}
}

func TestWeightedReset(t *testing.T) {
	s := &Weighted{Weights: []uint64{0, 1, 0, 0, 0}}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.SampleReplace() != 1 {
		t.Fatalf("Wrong sample")
	}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.Sample() != 1 {
		t.Fatalf("Wrong sample")
	}
	if s.CanSample() {
		t.Fatalf("Shouldn't be able to sample")
	}

	s.Replace()

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.SampleReplace() != 1 {
		t.Fatalf("Wrong sample")
	}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.Sample() != 1 {
		t.Fatalf("Wrong sample")
	}
	if s.CanSample() {
		t.Fatalf("Shouldn't be able to sample")
	}

	s.Weights = []uint64{0, 0, 1, 0, 0}
	s.Replace()

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.SampleReplace() != 2 {
		t.Fatalf("Wrong sample")
	}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.Sample() != 2 {
		t.Fatalf("Wrong sample")
	}
	if s.CanSample() {
		t.Fatalf("Shouldn't be able to sample")
	}
}
