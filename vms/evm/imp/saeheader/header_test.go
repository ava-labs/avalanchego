// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saeheader

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
)

func TestSettled(t *testing.T) {
	var (
		height       = uint64(7)
		gasUnix      = uint64(1_000)
		gasNumerator = uint64(3)
		excess       = uint64(42)
	)
	tests := []struct {
		name                                  string
		height, gasUnix, gasNumerator, excess *uint64
		want                                  hook.Settled
	}{
		{
			name: "absent",
			want: hook.Settled{},
		},
		{
			name:         "partially_set",
			height:       &height,
			gasUnix:      &gasUnix,
			gasNumerator: &gasNumerator,
			want:         hook.Settled{},
		},
		{
			name:         "set",
			height:       &height,
			gasUnix:      &gasUnix,
			gasNumerator: &gasNumerator,
			excess:       &excess,
			want: hook.Settled{
				Height:       7,
				GasUnix:      1_000,
				GasNumerator: gas.Gas(3),
				Excess:       gas.Gas(42),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Settled(tt.height, tt.gasUnix, tt.gasNumerator, tt.excess)
			require.Equal(t, tt.want, got, "Settled()")
		})
	}
}

func TestTime(t *testing.T) {
	tests := []struct {
		name      string
		seconds   uint64
		ms        uint64
		wantMilli int64
	}{
		{
			// Headers without a millisecond field decode it as zero.
			name:      "no_milliseconds",
			seconds:   1_700_000_000,
			wantMilli: 1_700_000_000_000,
		},
		{
			name:      "consistent_milliseconds",
			seconds:   100,
			ms:        100_500,
			wantMilli: 100_500,
		},
		{
			// The millisecond field encodes 105.500s while the seconds field
			// says 100s (e.g. from a malicious peer). Only the 500ms sub-second
			// remainder is honored.
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
