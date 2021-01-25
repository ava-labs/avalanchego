// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPopulateStringFields(t *testing.T) {
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
		A string
		B int
		C string
		k string
	}

	type type3 struct {
		b int64
	}

	t.Run("no fields", func(t *testing.T) {
		t1 := type1{}
		assert.NoError(t, PopulateStringFields(m, &t1))
		assert.Equal(t, type1{}, t1)
	})

	t.Run("mixed fields", func(t *testing.T) {
		t2 := type2{}
		assert.NoError(t, PopulateStringFields(m, &t2))
		assert.Equal(t, type2{A: "neat", C: `{"cool":"sweet","wow":1231}`}, t2)
	})

	t.Run("no string fields", func(t *testing.T) {
		t3 := type3{}
		assert.NoError(t, PopulateStringFields(m, &t3))
		assert.Equal(t, type3{}, t3)
	})
}
