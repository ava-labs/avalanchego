// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	os.Exit(m.Run())
}

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    extras.AvalancheRules
		extra    []byte
		expected []byte
	}{
		{
			name:     "empty_extra",
			extra:    nil,
			expected: nil,
		},
		{
			name:     "too_short",
			extra:    make([]byte, ap3WindowSize-1),
			expected: nil,
		},
		{
			name:     "empty_predicate",
			extra:    make([]byte, ap3WindowSize),
			expected: nil,
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				ap3WindowSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "fortuna_empty_extra",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "fortuna_too_short",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176StateSize-1),
			expected: nil,
		},
		{
			name: "fortuna_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176StateSize),
			expected: nil,
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				acp176StateSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "helicon_empty_predicate",
			rules: extras.AvalancheRules{
				IsHelicon: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "helicon_non_empty_predicate",
			rules: extras.AvalancheRules{
				IsHelicon: true,
			},
			extra:    []byte{5},
			expected: []byte{5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PredicateBytesFromExtra(test.rules, test.extra)
			require.Equal(t, test.expected, got)
		})
	}
}

func TestTargetExponentFromExtra(t *testing.T) {
	// Non-zero capacity and excess catch reading the wrong field.
	feeState := binary.BigEndian.AppendUint64(
		bytes.Repeat([]byte{0xff}, acp176StateSize-wrappers.LongLen),
		5_000, // target excess
	)

	tests := []struct {
		name    string
		extra   []byte
		want    dynamic.TargetExponent
		wantErr error
	}{
		{
			name:    "empty",
			wantErr: ErrInsufficientFeeState,
		},
		{
			name:    "too_short",
			extra:   feeState[:acp176StateSize-1],
			wantErr: ErrInsufficientFeeState,
		},
		{
			name:  "fee_state",
			extra: feeState,
			want:  5_000,
		},
		{
			name:  "fee_state_and_predicate",
			extra: append(bytes.Clone(feeState), 1, 2, 3),
			want:  5_000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TargetExponentFromExtra(tt.extra)
			require.ErrorIs(t, err, tt.wantErr, "TargetExponentFromExtra()")
			require.Equal(t, tt.want, got, "TargetExponentFromExtra()")
		})
	}
}
