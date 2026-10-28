// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPointerEqual(t *testing.T) {
	tests := []struct {
		name string
		x    *int
		y    *int
		want bool
	}{
		{
			name: "both_nil",
			want: true,
		},
		{
			name: "x_nil",
			y:    new(1),
			want: false,
		},
		{
			name: "y_nil",
			x:    new(1),
			want: false,
		},
		{
			name: "equal_values",
			x:    new(1),
			y:    new(1),
			want: true,
		},
		{
			name: "different_values",
			x:    new(1),
			y:    new(2),
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, PtrEqual(test.x, test.y))
		})
	}
}
