// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"strconv"
	"testing"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/require"
)

func TestDynamicFeeWindow_Add(t *testing.T) {
	tests := []struct {
		name     string
		window   DynamicFeeWindow
		amount   uint64
		expected DynamicFeeWindow
	}{
		{
			name: "normal_addition",
			window: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			amount: 5,
			expected: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 15,
			},
		},
		{
			name: "amount_overflow",
			window: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			amount: math.MaxUint64,
			expected: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, math.MaxUint64,
			},
		},
		{
			name: "window_overflow",
			window: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, math.MaxUint64,
			},
			amount: 5,
			expected: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, math.MaxUint64,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.window.Add(test.amount)
			require.Equal(t, test.expected, test.window)
		})
	}
}

func TestDynamicFeeWindow_Shift(t *testing.T) {
	window := DynamicFeeWindow{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
	}
	tests := []struct {
		n        uint64
		expected DynamicFeeWindow
	}{
		{
			n:        0,
			expected: window,
		},
		{
			n: 1,
			expected: DynamicFeeWindow{
				2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 2,
			expected: DynamicFeeWindow{
				3, 4, 5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 3,
			expected: DynamicFeeWindow{
				4, 5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 4,
			expected: DynamicFeeWindow{
				5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 5,
			expected: DynamicFeeWindow{
				6, 7, 8, 9, 10,
			},
		},
		{
			n: 6,
			expected: DynamicFeeWindow{
				7, 8, 9, 10,
			},
		},
		{
			n: 7,
			expected: DynamicFeeWindow{
				8, 9, 10,
			},
		},
		{
			n: 8,
			expected: DynamicFeeWindow{
				9, 10,
			},
		},
		{
			n: 9,
			expected: DynamicFeeWindow{
				10,
			},
		},
		{
			n:        10,
			expected: DynamicFeeWindow{},
		},
		{
			n:        100,
			expected: DynamicFeeWindow{},
		},
	}
	for _, test := range tests {
		t.Run(strconv.FormatUint(test.n, 10), func(t *testing.T) {
			window := window
			window.Shift(test.n)
			require.Equal(t, test.expected, window)
		})
	}
}

func TestDynamicFeeWindow_Sum(t *testing.T) {
	tests := []struct {
		name     string
		window   DynamicFeeWindow
		expected uint64
	}{
		{
			name:     "empty",
			window:   DynamicFeeWindow{},
			expected: 0,
		},
		{
			name: "full",
			window: DynamicFeeWindow{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			expected: 55,
		},
		{
			name: "overflow",
			window: DynamicFeeWindow{
				math.MaxUint64, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			expected: math.MaxUint64,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.window.Sum())
		})
	}
}

func TestDynamicFeeWindow_Bytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    []byte
		window   DynamicFeeWindow
		parseErr error
	}{
		{
			name:     "insufficient_length",
			bytes:    make([]byte, params.DynamicFeeExtraDataSize-1),
			parseErr: ErrDynamicFeeWindowInsufficientLength,
		},
		{
			name:   "zero_window",
			bytes:  make([]byte, params.DynamicFeeExtraDataSize),
			window: DynamicFeeWindow{},
		},
		{
			name: "truncate_bytes",
			bytes: []byte{
				params.DynamicFeeExtraDataSize: 1,
			},
			window: DynamicFeeWindow{},
		},
		{
			name: "endianess",
			bytes: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
				0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
				0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
				0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
				0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
				0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
			},
			window: DynamicFeeWindow{
				0x0102030405060708,
				0x1112131415161718,
				0x2122232425262728,
				0x3132333435363738,
				0x4142434445464748,
				0x5152535455565758,
				0x6162636465666768,
				0x7172737475767778,
				0x8182838485868788,
				0x9192939495969798,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			window, err := ParseDynamicFeeWindow(test.bytes)
			require.Equal(test.window, window)
			require.ErrorIs(err, test.parseErr)
			if test.parseErr != nil {
				return
			}

			expectedBytes := test.bytes[:params.DynamicFeeExtraDataSize]
			bytes := window.Bytes()
			require.Equal(expectedBytes, bytes)
		})
	}
}
