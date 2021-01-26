// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyBytesNil(t *testing.T) {
	result := CopyBytes(nil)
	assert.Nil(t, result, "CopyBytes(nil) should have returned nil")
}

func TestCopyBytes(t *testing.T) {
	input := []byte{1}
	result := CopyBytes(input)
	assert.Equal(t, input, result, "CopyBytes should have returned equal bytes")

	input[0] = 0
	assert.NotEqual(t, input, result, "CopyBytes should have returned independent bytes")
}

func TestSetByteSlices(t *testing.T) {
	m := map[string]interface{}{
		"k": "hello",
		"d": 10,
		"a": "neat",
		"c": map[string]interface{}{
			"wow":  1231,
			"cool": "sweet",
		},
	}

	type type1 struct{}

	type type2 struct {
		A []byte
		B int
		C []byte
		k []byte
		D []int
		Z []byte
	}

	type type3 struct {
		b int64
	}

	t.Run("no fields", func(t *testing.T) {
		t1 := type1{}
		assert.NoError(t, SetByteSlices(m, &t1))
		assert.Equal(t, type1{}, t1)
	})

	t.Run("mixed fields", func(t *testing.T) {
		t2 := type2{}
		assert.NoError(t, SetByteSlices(m, &t2))
		assert.Equal(t, type2{A: []byte("neat"), C: []byte(`{"cool":"sweet","wow":1231}`)}, t2)
	})

	t.Run("no string fields", func(t *testing.T) {
		t3 := type3{}
		assert.NoError(t, SetByteSlices(m, &t3))
		assert.Equal(t, type3{}, t3)
	})

	t.Run("non-struct", func(t *testing.T) {
		var s string
		err := SetByteSlices(m, &s)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot set byte slices on string")
	})
}
