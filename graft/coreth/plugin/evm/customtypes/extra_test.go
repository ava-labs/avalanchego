// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name  string
		rules cparams.AvalancheRules
		extra []byte
		want  []byte
	}{
		{
			name: "empty_extra",
		},
		{
			name:  "too_short",
			extra: make([]byte, ap3WindowSize-1),
		},
		{
			name:  "empty_predicate",
			extra: make([]byte, ap3WindowSize),
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				ap3WindowSize: 5,
			},
			want: []byte{5},
		},
		{
			name: "fortuna_empty_extra",
			rules: cparams.AvalancheRules{
				IsFortuna: true,
			},
		},
		{
			name: "fortuna_too_short",
			rules: cparams.AvalancheRules{
				IsFortuna: true,
			},
			extra: make([]byte, acp176StateSize-1),
		},
		{
			name: "fortuna_empty_predicate",
			rules: cparams.AvalancheRules{
				IsFortuna: true,
			},
			extra: make([]byte, acp176StateSize),
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: cparams.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				acp176StateSize: 5,
			},
			want: []byte{5},
		},
		{
			name: "helicon_empty_predicate",
			rules: cparams.AvalancheRules{
				IsHelicon: true,
			},
		},
		{
			name: "helicon_non_empty_predicate",
			rules: cparams.AvalancheRules{
				IsHelicon: true,
			},
			extra: []byte{5},
			want:  []byte{5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PredicateBytesFromExtra(tt.rules, tt.extra)
			require.Equal(t, tt.want, got, "PredicateBytesFromExtra()")
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
			name:  "fee_state",
			extra: feeState,
			want:  5_000,
		},
		{
			name:  "fee_state_and_predicate",
			extra: append(bytes.Clone(feeState), 1, 2, 3),
			want:  5_000,
		},
		{
			name:    "short_extra",
			extra:   feeState[:len(feeState)-1],
			wantErr: ErrInsufficientFeeState,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TargetExponentFromExtra(tt.extra)
			require.ErrorIs(t, err, tt.wantErr, "TargetExponentFromExtra()")
			assert.Equal(t, tt.want, got, "TargetExponentFromExtra()")
		})
	}
}
