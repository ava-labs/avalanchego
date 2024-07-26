// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec/codectest"
)

func TestVectors(t *testing.T) {
	for _, test := range codectest.Tests {
		c := NewDefault()
		test(c, t)
	}
}

func TestMultipleTags(t *testing.T) {
	for _, test := range codectest.MultipleTagsTests {
		c := New([]string{"tag1", "tag2"})
		test(c, t)
	}
}

func FuzzStructUnmarshalLinearCodec(f *testing.F) {
	c := NewDefault()
	codectest.FuzzStructUnmarshal(c, f)
}
