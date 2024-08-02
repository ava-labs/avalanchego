// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hierarchycodec

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec/codectest"
)

func TestVectors(t *testing.T) {
	for _, test := range codectest.Tests {
		t.Run("", func(t *testing.T) {
			c := NewDefault()
			test(t, c)
		})
	}
}

func TestMultipleTags(t *testing.T) {
	for _, test := range codectest.MultipleTagsTests {
		t.Run("", func(t *testing.T) {
			c := New([]string{"tag1", "tag2"})
			test(t, c)
		})
	}
}

func FuzzStructUnmarshalHierarchyCodec(f *testing.F) {
	c := NewDefault()
	codectest.FuzzStructUnmarshal(c, f)
}
