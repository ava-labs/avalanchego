// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"fmt"
	"math"
	"testing"
)

const (
	countSize  = 5
	subsetSize = 3

	iterations = 1000
	threshold  = 100
)

func TestUniform(t *testing.T) {
	counts := [countSize]int{}
	for i := 0; i < iterations; i++ {
		s := &Uniform{N: 5}
		subset := Subset(s, subsetSize)
		for _, j := range subset {
			counts[j]++
		}
		if len(subset) != subsetSize {
			t.Fatalf("Incorrect size")
		}
	}

	expected := iterations * float64(subsetSize) / countSize
	for i := 0; i < countSize; i++ {
		if math.Abs(float64(counts[i])-expected) > threshold {
			t.Fatalf("Index seems biased: %s", fmt.Sprint(counts))
		}
	}
}

func TestUniformReset(t *testing.T) {
	s := &Uniform{N: 1}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.SampleReplace() != 0 {
		t.Fatalf("Wrong sample")
	}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.Sample() != 0 {
		t.Fatalf("Wrong sample")
	}
	if s.CanSample() {
		t.Fatalf("Shouldn't be able to sample")
	}

	s.Replace()

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.SampleReplace() != 0 {
		t.Fatalf("Wrong sample")
	}

	if !s.CanSample() {
		t.Fatalf("Should be able to sample")
	}
	if s.Sample() != 0 {
		t.Fatalf("Wrong sample")
	}
	if s.CanSample() {
		t.Fatalf("Shouldn't be able to sample")
	}
}
