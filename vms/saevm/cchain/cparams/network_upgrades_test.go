// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTimestampForked(t *testing.T) {
	tests := []struct {
		name     string
		fork     *uint64
		head     uint64
		isForked bool
	}{
		{
			name:     "nil fork at 0",
			fork:     nil,
			head:     0,
			isForked: false,
		},
		{
			name:     "nil fork at non-zero",
			fork:     nil,
			head:     100,
			isForked: false,
		},
		{
			name:     "zero fork at genesis",
			fork:     new(uint64),
			head:     0,
			isForked: true,
		},
		{
			name:     "pre fork timestamp",
			fork:     new(uint64(100)),
			head:     50,
			isForked: false,
		},
		{
			name:     "at fork timestamp",
			fork:     new(uint64(100)),
			head:     100,
			isForked: true,
		},
		{
			name:     "post fork timestamp",
			fork:     new(uint64(100)),
			head:     150,
			isForked: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := isTimestampForked(tt.fork, tt.head)
			require.Equal(t, tt.isForked, res)
		})
	}
}
