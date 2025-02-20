// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ap3

import (
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common/math"
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
