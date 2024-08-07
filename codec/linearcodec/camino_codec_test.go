// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec"
)

func TestVectorsCamino(t *testing.T) {
	for _, test := range codec.Tests {
		c := NewCaminoDefault()
		test(c, t)
	}
}

func TestMultipleTagsCamino(t *testing.T) {
	for _, test := range codec.MultipleTagsTests {
		c := NewCamino([]string{"tag1", "tag2"}, defaultMaxSliceLength)
		test(c, t)
	}
}

func TestVersionCamino(t *testing.T) {
	for _, test := range codec.VersionTests {
		c := NewCaminoDefault()
		test(c, t)
	}
}
