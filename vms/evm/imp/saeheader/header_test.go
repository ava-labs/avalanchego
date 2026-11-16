// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saeheader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTime(t *testing.T) {
	tests := []struct {
		name      string
		seconds   uint64
		ms        uint64
		wantMilli int64
	}{
		{
			name:      "consistent_milliseconds",
			seconds:   100,
			ms:        100_500,
			wantMilli: 100_500,
		},
		{
			// The extra 5s in ms is ignored.
			name:      "milliseconds_disagreeing_on_second",
			seconds:   100,
			ms:        105_500,
			wantMilli: 100_500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantMilli, Time(tt.seconds, tt.ms).UnixMilli(), "Time().UnixMilli()")
		})
	}
}
