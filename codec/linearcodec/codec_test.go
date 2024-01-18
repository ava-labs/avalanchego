// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestVectors(t *testing.T) {
	for _, test := range codec.Tests {
		c := NewDefault(mockable.MaxTime)
		test(c, t)
	}
}

func TestMultipleTags(t *testing.T) {
	for _, test := range codec.MultipleTagsTests {
		c := New(mockable.MaxTime, []string{"tag1", "tag2"}, DefaultMaxSliceLength)
		test(c, t)
	}
}

func TestEnforceSliceLen(t *testing.T) {
	for _, test := range codec.EnforceSliceLenTests {
		c := NewDefault(mockable.MaxTime)
		test(c, t)
	}
}

func TestIgnoreSliceLen(t *testing.T) {
	for _, test := range codec.IgnoreSliceLenTests {
		c := NewDefault(time.Time{})
		test(c, t)
	}
}

func FuzzStructUnmarshalLinearCodec(f *testing.F) {
	c := NewDefault(mockable.MaxTime)
	codec.FuzzStructUnmarshal(c, f)
}
