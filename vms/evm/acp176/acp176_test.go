// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp176

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

var parseTests = []struct {
	name        string
	bytes       []byte
	state       State
	expectedErr error
}{
	{
		name:        "insufficient_length",
		bytes:       make([]byte, StateSize-1),
		expectedErr: ErrStateInsufficientLength,
	},
	{
		name:  "zero_state",
		bytes: make([]byte, StateSize),
		state: State{},
	},
	{
		name: "truncate_bytes",
		bytes: []byte{
			StateSize: 1,
		},
		state: State{},
	},
	{
		name: "endianess",
		bytes: []byte{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		},
		state: State{
			Gas: gas.State{
				Capacity: 0x0102030405060708,
				Excess:   0x1112131415161718,
			},
			TargetExcess: 0x2122232425262728,
		},
	},
}

func TestParseState(t *testing.T) {
	for _, test := range parseTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state, err := ParseState(test.bytes)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.state, state)
		})
	}
}

func BenchmarkParseState(b *testing.B) {
	for _, test := range parseTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_, _ = ParseState(test.bytes)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	for _, test := range parseTests {
		if test.expectedErr != nil {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			expectedBytes := test.bytes[:StateSize]
			bytes := test.state.Bytes()
			require.Equal(t, expectedBytes, bytes)
		})
	}
}

func BenchmarkBytes(b *testing.B) {
	for _, test := range parseTests {
		if test.expectedErr != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_ = test.state.Bytes()
			}
		})
	}
}
