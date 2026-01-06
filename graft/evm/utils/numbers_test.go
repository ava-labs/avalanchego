// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBigEqual(t *testing.T) {
	tests := []struct {
		name string
		a    *big.Int
		b    *big.Int
		want bool
	}{
		{
			name: "nil_nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "0_nil",
			a:    big.NewInt(0),
			b:    nil,
			want: false,
		},
		{
			name: "0_1",
			a:    big.NewInt(0),
			b:    big.NewInt(1),
			want: false,
		},
		{
			name: "1_1",
			a:    big.NewInt(1),
			b:    big.NewInt(1),
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.want, BigEqual(test.a, test.b))
			require.Equal(test.want, BigEqual(test.b, test.a))
		})
	}
}

func TestBigEqualUint64(t *testing.T) {
	tests := []struct {
		name string
		a    *big.Int
		b    uint64
		want bool
	}{
		{
			name: "nil",
			a:    nil,
			b:    0,
			want: false,
		},
		{
			name: "not_uint64",
			a:    big.NewInt(-1),
			b:    0,
			want: false,
		},
		{
			name: "equal",
			a:    big.NewInt(1),
			b:    1,
			want: true,
		},
		{
			name: "not_equal",
			a:    big.NewInt(1),
			b:    2,
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := BigEqualUint64(test.a, test.b)
			require.Equal(t, test.want, got)
		})
	}
}

func TestLessOrEqualUint64(t *testing.T) {
	tests := []struct {
		name string
		a    *big.Int
		b    uint64
		want bool
	}{
		{
			name: "nil",
			a:    nil,
			b:    0,
			want: false,
		},
		{
			name: "not_uint64",
			a:    big.NewInt(-1),
			b:    0,
			want: false,
		},
		{
			name: "less",
			a:    big.NewInt(1),
			b:    2,
			want: true,
		},
		{
			name: "equal",
			a:    big.NewInt(1),
			b:    1,
			want: true,
		},
		{
			name: "greater",
			a:    big.NewInt(2),
			b:    1,
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := BigLessOrEqualUint64(test.a, test.b)
			require.Equal(t, test.want, got)
		})
	}
}
