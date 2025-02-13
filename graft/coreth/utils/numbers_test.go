// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			assert.Equal(t, test.want, got)
		})
	}
}
