// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSetSet(t *testing.T) {
	vdr0 := NewValidator(ids.ShortEmpty, 1)
	vdr1 := NewValidator(ids.ShortID{0xFF}, math.MaxInt64-1)
	// Should be discarded, because it has a weight of 0
	vdr2 := NewValidator(ids.ShortID{0xAA}, 0)

	s := NewSet()
	err := s.Set([]Validator{vdr0, vdr1, vdr2})
	assert.NoError(t, err)

	length := s.Len()
	assert.Equal(t, 2, length, "should have two validators")

	contains := s.Contains(vdr0.ID())
	assert.True(t, contains, "should have contained vdr0")

	contains = s.Contains(vdr1.ID())
	assert.True(t, contains, "should have contained vdr1")

	sampled, err := s.Sample(1)
	assert.NoError(t, err)
	assert.Len(t, sampled, 1, "should have only sampled one validator")
	assert.Equal(t, vdr1.ID(), sampled[0].ID(), "should have sampled vdr1")
}

func TestSamplerSample(t *testing.T) {
	vdr0 := ids.GenerateTestShortID()
	vdr1 := ids.GenerateTestShortID()

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	assert.NoError(t, err)

	sampled, err := s.Sample(1)
	assert.NoError(t, err)
	assert.Len(t, sampled, 1, "should have only sampled one validator")
	assert.Equal(t, vdr0, sampled[0].ID(), "should have sampled vdr0")

	_, err = s.Sample(2)
	assert.Error(t, err, "should have errored during sampling")

	err = s.AddWeight(vdr1, math.MaxInt64-1)
	assert.NoError(t, err)

	sampled, err = s.Sample(1)
	assert.NoError(t, err)
	assert.Len(t, sampled, 1, "should have only sampled one validator")
	assert.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")

	sampled, err = s.Sample(2)
	assert.NoError(t, err)
	assert.Len(t, sampled, 2, "should have sampled two validators")
	assert.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")
	assert.Equal(t, vdr1, sampled[1].ID(), "should have sampled vdr1")

	sampled, err = s.Sample(3)
	assert.NoError(t, err)
	assert.Len(t, sampled, 3, "should have sampled three validators")
	assert.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")
	assert.Equal(t, vdr1, sampled[1].ID(), "should have sampled vdr1")
	assert.Equal(t, vdr1, sampled[2].ID(), "should have sampled vdr1")
}

func TestSamplerDuplicate(t *testing.T) {
	vdr0 := ids.GenerateTestShortID()
	vdr1 := ids.GenerateTestShortID()

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	assert.NoError(t, err)

	err = s.AddWeight(vdr1, 1)
	assert.NoError(t, err)

	err = s.AddWeight(vdr1, math.MaxInt64-2)
	assert.NoError(t, err)

	sampled, err := s.Sample(1)
	assert.NoError(t, err)
	assert.Len(t, sampled, 1, "should have only sampled one validator")
	assert.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")
}

func TestSamplerContains(t *testing.T) {
	vdr := ids.GenerateTestShortID()

	s := NewSet()
	err := s.AddWeight(vdr, 1)
	assert.NoError(t, err)

	contains := s.Contains(vdr)
	assert.True(t, contains, "should have contained validator")

	err = s.RemoveWeight(vdr, 1)
	assert.NoError(t, err)

	contains = s.Contains(vdr)
	assert.False(t, contains, "shouldn't have contained validator")
}

func TestSamplerString(t *testing.T) {
	vdr0 := ids.ShortEmpty
	vdr1 := ids.ShortID{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	assert.NoError(t, err)

	err = s.AddWeight(vdr1, math.MaxInt64-1)
	assert.NoError(t, err)

	expected := "Validator Set: (Size = 2, SampleableWeight = 9223372036854775807, Weight = 9223372036854775807)\n" +
		"    Validator[0]:        111111111111111111116DBWJs, 1/1\n" +
		"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806/9223372036854775806"
	result := s.String()
	assert.Equal(t, expected, result, "wrong string returned")
}

func TestSetWeight(t *testing.T) {
	vdr0 := ids.ShortID{1}
	weight0 := uint64(93)
	vdr1 := ids.ShortID{2}
	weight1 := uint64(123)

	s := NewSet()
	err := s.AddWeight(vdr0, weight0)
	assert.NoError(t, err)

	err = s.AddWeight(vdr1, weight1)
	assert.NoError(t, err)

	setWeight := s.Weight()
	expectedWeight := weight0 + weight1
	assert.Equal(t, expectedWeight, setWeight, "wrong set weight")
}

func TestSetSubsetWeight(t *testing.T) {
	vdr0 := ids.ShortID{1}
	weight0 := uint64(93)
	vdr1 := ids.ShortID{2}
	weight1 := uint64(123)
	vdr2 := ids.ShortID{3}
	weight2 := uint64(810)
	subset := ids.ShortSet{}
	subset.Add(vdr0)
	subset.Add(vdr1)

	s := NewSet()
	err := s.AddWeight(vdr0, weight0)
	assert.NoError(t, err)

	err = s.AddWeight(vdr1, weight1)
	assert.NoError(t, err)
	err = s.AddWeight(vdr2, weight2)
	assert.NoError(t, err)

	subsetWeight, err := s.SubsetWeight(subset)
	if err != nil {
		t.Fatal(err)
	}
	expectedWeight := weight0 + weight1
	assert.Equal(t, expectedWeight, subsetWeight, "wrong subset weight")
}

func TestSamplerMasked(t *testing.T) {
	vdr0 := ids.ShortEmpty
	vdr1 := ids.ShortID{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	assert.NoError(t, err)

	err = s.MaskValidator(vdr1)
	assert.NoError(t, err)

	{
		expected := "Validator Set: (Size = 1, SampleableWeight = 1, Weight = 1)\n" +
			"    Validator[0]:        111111111111111111116DBWJs, 1/1"
		result := s.String()
		assert.Equal(t, expected, result, "wrong string returned")
	}

	err = s.AddWeight(vdr1, math.MaxInt64-1)
	assert.NoError(t, err)

	{
		expected := "Validator Set: (Size = 2, SampleableWeight = 1, Weight = 9223372036854775807)\n" +
			"    Validator[0]:        111111111111111111116DBWJs, 1/1\n" +
			"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 0/9223372036854775806"
		result := s.String()
		assert.Equal(t, expected, result, "wrong string returned")
	}

	err = s.RevealValidator(vdr1)
	assert.NoError(t, err)

	{
		expected := "Validator Set: (Size = 2, SampleableWeight = 9223372036854775807, Weight = 9223372036854775807)\n" +
			"    Validator[0]:        111111111111111111116DBWJs, 1/1\n" +
			"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806/9223372036854775806"
		result := s.String()
		assert.Equal(t, expected, result, "wrong string returned")
	}

	err = s.MaskValidator(vdr1)
	assert.NoError(t, err)

	{
		expected := "Validator Set: (Size = 2, SampleableWeight = 1, Weight = 9223372036854775807)\n" +
			"    Validator[0]:        111111111111111111116DBWJs, 1/1\n" +
			"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 0/9223372036854775806"
		result := s.String()
		assert.Equal(t, expected, result, "wrong string returned")
	}

	err = s.RevealValidator(vdr1)
	assert.NoError(t, err)

	{
		expected := "Validator Set: (Size = 2, SampleableWeight = 9223372036854775807, Weight = 9223372036854775807)\n" +
			"    Validator[0]:        111111111111111111116DBWJs, 1/1\n" +
			"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806/9223372036854775806"
		result := s.String()
		assert.Equal(t, expected, result, "wrong string returned")
	}

	err = s.RevealValidator(vdr1)
	assert.NoError(t, err)

	{
		expected := "Validator Set: (Size = 2, SampleableWeight = 9223372036854775807, Weight = 9223372036854775807)\n" +
			"    Validator[0]:        111111111111111111116DBWJs, 1/1\n" +
			"    Validator[1]: QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806/9223372036854775806"
		result := s.String()
		assert.Equal(t, expected, result, "wrong string returned")
	}
}
