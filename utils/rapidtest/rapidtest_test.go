// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rapidtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinSeed(t *testing.T) {
	tests := []struct {
		name    string
		seedSet bool
		shuffle string
		wantPin bool
	}{
		{"explicit seed", true, "off", false},
		{"shuffle on", false, "on", false},
		{"shuffle seed", false, "12345", false},
		{"defaults", false, "off", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seed, pin := pinSeed(tt.seedSet, tt.shuffle)
			require.Equal(t, tt.wantPin, pin, "pinSeed(%v, %q) pin", tt.seedSet, tt.shuffle)
			if tt.wantPin {
				require.Equal(t, FixedSeed, seed, "pinSeed() seed")
			}
		})
	}
}
