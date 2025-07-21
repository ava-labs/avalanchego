// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"strconv"
	"testing"

	"github.com/ava-labs/libevm/common/math"
	"github.com/stretchr/testify/require"
)

func TestWindow_Add(t *testing.T) {
	tests := []struct {
		name     string
		window   Window
		amount   uint64
		expected Window
	}{
		{
			name: "normal_addition",
			window: Window{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			amount: 5,
			expected: Window{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 15,
			},
		},
		{
			name: "amount_overflow",
			window: Window{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			amount: math.MaxUint64,
			expected: Window{
				1, 2, 3, 4, 5, 6, 7, 8, 9, math.MaxUint64,
			},
		},
		{
			name: "window_overflow",
			window: Window{
				1, 2, 3, 4, 5, 6, 7, 8, 9, math.MaxUint64,
			},
			amount: 5,
			expected: Window{
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

func TestWindow_Shift(t *testing.T) {
	window := Window{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
	}
	tests := []struct {
		n        uint64
		expected Window
	}{
		{
			n:        0,
			expected: window,
		},
		{
			n: 1,
			expected: Window{
				2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 2,
			expected: Window{
				3, 4, 5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 3,
			expected: Window{
				4, 5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 4,
			expected: Window{
				5, 6, 7, 8, 9, 10,
			},
		},
		{
			n: 5,
			expected: Window{
				6, 7, 8, 9, 10,
			},
		},
		{
			n: 6,
			expected: Window{
				7, 8, 9, 10,
			},
		},
		{
			n: 7,
			expected: Window{
				8, 9, 10,
			},
		},
		{
			n: 8,
			expected: Window{
				9, 10,
			},
		},
		{
			n: 9,
			expected: Window{
				10,
			},
		},
		{
			n:        10,
			expected: Window{},
		},
		{
			n:        100,
			expected: Window{},
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

func TestWindow_Sum(t *testing.T) {
	tests := []struct {
		name     string
		window   Window
		expected uint64
	}{
		{
			name:     "empty",
			window:   Window{},
			expected: 0,
		},
		{
			name: "full",
			window: Window{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			},
			expected: 55,
		},
		{
			name: "overflow",
			window: Window{
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

func TestWindow_Bytes(t *testing.T) {
	tests := []struct {
		name    string
		bytes   []byte
		want    Window
		wantErr error
	}{
		{
			name:    "insufficient_length",
			bytes:   make([]byte, WindowSize-1),
			wantErr: ErrWindowInsufficientLength,
		},
		{
			name:  "zero_window",
			bytes: make([]byte, WindowSize),
			want:  Window{},
		},
		{
			name: "truncate_bytes",
			bytes: []byte{
				WindowSize: 1,
			},
			want: Window{},
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
			want: Window{
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

			window, err := ParseWindow(test.bytes)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, window)
			if test.wantErr != nil {
				return
			}

			expectedBytes := test.bytes[:WindowSize]
			bytes := window.Bytes()
			require.Equal(expectedBytes, bytes)
		})
	}
}
