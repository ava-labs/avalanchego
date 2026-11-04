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
