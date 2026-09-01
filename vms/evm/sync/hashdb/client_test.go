// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"context"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// This test verifies that the client drops invalid responses and retries the
// request until a response verifies or cancellation ends the fetch.
func TestClient_Retries(t *testing.T) {
	t.Parallel()

	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, maxLimit)

	const cancelAfter = 3
	tests := []struct {
		name   string
		numBad int

		want    Leaves
		wantErr error
	}{
		{
			name:   "recovers_from_invalid_response",
			numBad: 1,
			want: Leaves{
				Keys: keys,
				Vals: vals,
			},
		},
		{
			name:    "cancellation_ends_retries",
			numBad:  cancelAfter,
			want:    Leaves{},
			wantErr: context.Canceled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			log := loggingtest.New(t, logging.Debug)
			responder := newLeafResponder(t, trieDB)
			tampering := synctest.NewMutatingResponder(responder, tt.numBad, func(resp *syncpb.GetLeafResponse) {
				*resp = syncpb.GetLeafResponse{} // Empty responses are invalid.
			})
			recording := synctest.NewRecordingResponder(tampering)
			net, tracker := synctest.ServeResponder(
				t,
				ctx,
				log,
				p2p.EVMLeafRequestHandlerID,
				synctest.NewCancelAfter(recording, cancelAfter, cancel),
			)
			client := NewClient(log, net, p2p.EVMLeafRequestHandlerID, common.HashLength, tracker, synctest.NewRequestMetrics(t))

			got, more, err := client.FetchLeaves(ctx, LeafRange{Root: root, Limit: maxLimit})
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, got, "a tampered range must never be returned")
			require.False(t, more)

			wantRequests := min(tt.numBad+1, cancelAfter)
			require.Len(t, recording.Requests(), wantRequests)
		})
	}
}

// This test verifies the error each corruption of a served response produces.
func TestVerifyRange(t *testing.T) {
	t.Parallel()

	const numSlots = 50
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, numSlots)
	responder := newLeafResponder(t, trieDB)

	const defaultLimit uint16 = 20
	minKey := make([]byte, common.HashLength)
	maxKey := slices.Repeat([]byte{0xff}, common.HashLength)
	tests := []struct {
		name     string
		start    []byte
		limit    uint16
		tamper   func(*syncpb.GetLeafResponse)
		wantMore bool
		wantErr  error
	}{
		{
			name:     "valid_response",
			wantMore: true,
		},
		{
			name:  "full_trie_valid_response",
			limit: numSlots,
		},
		{
			name:     "valid_from_start_key",
			start:    keys[10],
			wantMore: true,
		},
		{
			name:  "valid_to_trie_end",
			start: keys[numSlots-defaultLimit],
		},
		{
			name:  "valid_exclusion_only",
			start: maxKey,
		},
		{
			name: "incorrect_key",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys[0][0]++
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name:  "incorrect_value",
			start: keys[10],
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Values[0][0]++
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name:  "missing_first_slot",
			start: keys[10],
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys = resp.Keys[1:]
				resp.Values = resp.Values[1:]
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name: "truncated_response",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys = resp.Keys[:1]
				resp.Values = resp.Values[:1]
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name:  "mutated_last_key",
			start: keys[25],
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys[len(resp.Keys)-1] = maxKey
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name:  "key_before_start",
			start: keys[10],
			limit: numSlots,
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys, resp.Values = keys, vals
				resp.ProofVals = nil
			},
			wantErr: errKeyBeforeStart,
		},
		{
			name:  "exceeds_limit",
			start: keys[5],
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys = append(resp.Keys, resp.Keys[0])
				resp.Values = append(resp.Values, resp.Values[0])
			},
			wantErr: errTooManyLeaves,
		},
		{
			name:  "missing_proof",
			start: keys[35],
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.ProofVals = nil
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name: "empty_response",
			tamper: func(resp *syncpb.GetLeafResponse) {
				*resp = syncpb.GetLeafResponse{}
			},
			wantErr: errInvalidRangeProof,
		},
		{
			name: "forged_empty_range",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys = nil
				resp.Values = nil
			},
			wantErr: errInvalidRangeProof,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limit := defaultLimit
			if tt.limit > 0 {
				limit = tt.limit
			}
			resp, appErr := responder.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: tt.start,
				KeyLimit: uint32(limit),
			})
			require.Nil(t, appErr)
			if tt.tamper != nil {
				tt.tamper(resp)
			}

			more, err := verifyRange(
				minKey,
				LeafRange{
					Root:  root,
					Start: tt.start,
					Limit: limit,
				},
				resp,
			)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantMore, more)
		})
	}
}
