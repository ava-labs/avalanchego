// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestSetSet(t *testing.T) {
	vdr0 := NewValidator(ids.ShortEmpty, 1)
	vdr1_0 := NewValidator(ids.NewShortID([20]byte{0xFF}), 1)
	// Should replace vdr1_0, because later additions replace earlier ones
	vdr1_1 := NewValidator(ids.NewShortID([20]byte{0xFF}), math.MaxInt64-1)
	// Should be discarded, because it has a weight of 0
	vdr2 := NewValidator(ids.NewShortID([20]byte{0xAA}), 0)

	s := NewSet()
	s.Set([]Validator{vdr0, vdr1_0, vdr1_1, vdr2})

	if !s.Contains(vdr0.ID()) {
		t.Fatal("Should have contained vdr0", vdr0.ID())
	}
	if !s.Contains(vdr1_0.ID()) {
		t.Fatal("Should have contained vdr1", vdr1_0.ID())
	}
	if sampled := s.Sample(1); !sampled[0].ID().Equals(vdr1_0.ID()) {
		t.Fatal("Should have sampled vdr1")
	}
	if len := s.Len(); len != 2 {
		t.Fatalf("Got size %d, expected 2", len)
	}
}

func TestSamplerSample(t *testing.T) {
	vdr0 := GenerateRandomValidator(1)
	vdr1 := GenerateRandomValidator(math.MaxInt64 - 1)

	s := NewSet()
	s.Add(vdr0)

	if sampled := s.Sample(1); len(sampled) != 1 {
		t.Fatalf("Should have sampled 1 validator")
	} else if !sampled[0].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have sampled vdr0")
	} else if s.Len() != 1 {
		t.Fatalf("Wrong size")
	}

	s.Add(vdr1)

	if sampled := s.Sample(1); len(sampled) != 1 {
		t.Fatalf("Should have sampled 1 validator")
	} else if !sampled[0].ID().Equals(vdr1.ID()) {
		t.Fatalf("Should have sampled vdr1")
	} else if s.Len() != 2 {
		t.Fatalf("Wrong size")
	}

	if sampled := s.Sample(2); len(sampled) != 2 {
		t.Fatalf("Should have sampled 2 validators")
	} else if !sampled[1].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have sampled vdr0")
	} else if !sampled[0].ID().Equals(vdr1.ID()) {
		t.Fatalf("Should have sampled vdr1")
	}

	if sampled := s.Sample(3); len(sampled) != 2 {
		t.Fatalf("Should have sampled 2 validators")
	} else if !sampled[1].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have sampled vdr0")
	} else if !sampled[0].ID().Equals(vdr1.ID()) {
		t.Fatalf("Should have sampled vdr1")
	}

	if list := s.List(); len(list) != 2 {
		t.Fatalf("Should have returned 2 validators")
	} else if !list[0].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have returned vdr0")
	} else if !list[1].ID().Equals(vdr1.ID()) {
		t.Fatalf("Should have returned vdr1")
	}
}

func TestSamplerDuplicate(t *testing.T) {
	vdr0 := GenerateRandomValidator(1)
	vdr1_0 := GenerateRandomValidator(math.MaxInt64 - 1)
	vdr1_1 := NewValidator(vdr1_0.ID(), 0)

	s := NewSet()
	s.Add(vdr0)
	s.Add(vdr1_0)

	if sampled := s.Sample(1); len(sampled) != 1 {
		t.Fatalf("Should have sampled 1 validator")
	} else if !sampled[0].ID().Equals(vdr1_0.ID()) {
		t.Fatalf("Should have sampled vdr1")
	}

	s.Add(vdr1_1)

	if sampled := s.Sample(1); len(sampled) != 1 {
		t.Fatalf("Should have sampled 1 validator")
	} else if !sampled[0].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have sampled vdr0")
	}

	if sampled := s.Sample(2); len(sampled) != 1 {
		t.Fatalf("Should have only sampled 1 validator")
	} else if !sampled[0].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have sampled vdr0")
	}

	s.Remove(vdr1_1.ID())

	if sampled := s.Sample(2); len(sampled) != 1 {
		t.Fatalf("Should have only sampled 1 validator")
	} else if !sampled[0].ID().Equals(vdr0.ID()) {
		t.Fatalf("Should have sampled vdr0")
	}
}

func TestSamplerSimple(t *testing.T) {
	vdr := GenerateRandomValidator(1)

	s := NewSet()
	s.Add(vdr)

	if sampled := s.Sample(1); len(sampled) != 1 {
		t.Fatalf("Should have sampled 1 validator")
	}
}

func TestSamplerContains(t *testing.T) {
	vdr := GenerateRandomValidator(1)

	s := NewSet()
	s.Add(vdr)

	if !s.Contains(vdr.ID()) {
		t.Fatalf("Should have contained validator")
	}

	s.Remove(vdr.ID())

	if s.Contains(vdr.ID()) {
		t.Fatalf("Shouldn't have contained validator")
	}
}

func TestSamplerString(t *testing.T) {
	vdr0 := NewValidator(ids.ShortEmpty, 1)
	vdr1 := NewValidator(
		ids.NewShortID([20]byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		}),
		math.MaxInt64-1,
	)

	s := NewSet()
	s.Add(vdr0)
	s.Add(vdr1)

	expected := "Validator Set: (Size = 2)\n" +
		"    Validator[0]:        111111111111111111116DBWJs, 1\n" +
		"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806"
	if str := s.String(); str != expected {
		t.Fatalf("Got:\n%s\nExpected:\n%s", str, expected)
	}
}

func TestSetWeight(t *testing.T) {
	weight0 := uint64(93)
	vdr0 := NewValidator(ids.NewShortID([20]byte{1}), weight0)
	weight1 := uint64(123)
	vdr1 := NewValidator(ids.NewShortID([20]byte{2}), weight1)

	s := NewSet()
	s.Add(vdr0)
	s.Add(vdr1)

	setWeight, err := s.Weight()
	if err != nil {
		t.Fatalf("Failed to get weight of validators due to: %w", err)
	}

	expectedWeight := weight0 + weight1
	if setWeight != expectedWeight {
		t.Fatalf("Set weight was: %d, but expected: %d", setWeight, expectedWeight)
	}
}

func TestSetWeightErrors(t *testing.T) {
	weight0 := uint64(math.MaxUint64)
	vdr0 := NewValidator(ids.NewShortID([20]byte{1}), weight0)
	weight1 := uint64(123)
	vdr1 := NewValidator(ids.NewShortID([20]byte{2}), weight1)

	s := NewSet()
	s.Add(vdr0)
	s.Add(vdr1)

	_, err := s.Weight()
	if err == nil {
		t.Fatalf("Weight should have errored due to math overflow")
	}
}
