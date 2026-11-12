// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

func TestPredicateResults(t *testing.T) {
	results := predicate.BlockResults{
		common.Hash{1}: {common.Address{2}: set.NewBits(3)},
	}
	resultsBytes, err := results.Bytes()
	require.NoError(t, err, "BlockResults.Bytes()")

	// Non-zero prefixes catch the results being parsed from the wrong offset.
	ap3Window := bytes.Repeat([]byte{0xff}, ap3WindowSize)
	feeState := bytes.Repeat([]byte{0xff}, acp176StateSize)
	cat := func(a, b []byte) []byte { return append(bytes.Clone(a), b...) }

	tests := []struct {
		name   string
		format ExtraFormat
		extra  []byte
		want   predicate.BlockResults
	}{
		{
			name:   "ap3_empty_extra",
			format: AP3ExtraFormat,
		},
		{
			name:   "ap3_too_short",
			format: AP3ExtraFormat,
			extra:  ap3Window[1:],
		},
		{
			name:   "ap3_no_results",
			format: AP3ExtraFormat,
			extra:  ap3Window,
		},
		{
			name:   "ap3_results",
			format: AP3ExtraFormat,
			extra:  cat(ap3Window, resultsBytes),
			want:   results,
		},
		{
			name:   "fortuna_empty_extra",
			format: FortunaExtraFormat,
		},
		{
			name:   "fortuna_no_results",
			format: FortunaExtraFormat,
			extra:  feeState,
		},
		{
			name:   "fortuna_results",
			format: FortunaExtraFormat,
			extra:  cat(feeState, resultsBytes),
			want:   results,
		},
		{
			name:   "helicon_empty_extra",
			format: HeliconExtraFormat,
		},
		{
			name:   "helicon_results",
			format: HeliconExtraFormat,
			extra:  resultsBytes,
			want:   results,
		},
		{
			name:   "unparseable_results",
			format: HeliconExtraFormat,
			extra:  []byte{0xff},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ethtypes.Header{Extra: tt.extra}
			got := PredicateResults(h, tt.format)
			require.Equal(t, tt.want, got, "PredicateResults(%v)", tt.format)
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
