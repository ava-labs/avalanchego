// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

type blocks struct {
	accepted set.Set[ids.ID]
}

func newBlocks(ids ...ids.ID) blocks {
	return blocks{
		set.Of(ids...),
	}
}

func (b blocks) IsAccepted(_ context.Context, id ids.ID) error {
	if !b.accepted.Contains(id) {
		return database.ErrNotFound
	}
	return nil
}

func TestVerifier(t *testing.T) {
	addressedCallMsg, _ := newAddressedCall(t)
	hashMsg, hash := newHash(t)

	invalidPayloadMsg, err := warp.NewUnsignedMessage(networkID, sourceChainID, nil)
	require.NoError(t, err)

	tests := []struct {
		name             string
		acceptedBlocks   []ids.ID
		acceptedMessages []*warp.UnsignedMessage
		m                *warp.UnsignedMessage
		want             *common.AppError
	}{
		{
			name: "known_message",
			acceptedMessages: []*warp.UnsignedMessage{
				addressedCallMsg,
			},
			m: addressedCallMsg,
		},
		{
			name: "invalid_payload",
			m:    invalidPayloadMsg,
			want: &common.AppError{
				Code: ParseErrCode,
			},
		},
		{
			name: "wrong_payload_type",
			m:    addressedCallMsg,
			want: &common.AppError{
				Code: TypeErrCode,
			},
		},
		{
			name: "accepted_block",
			acceptedBlocks: []ids.ID{
				hash.Hash,
			},
			m: hashMsg,
		},
		{
			name: "unaccepted_block",
			m:    hashMsg,
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := NewVerifier(
				newBlocks(test.acceptedBlocks...),
				NewStorage(memdb.New(), test.acceptedMessages...),
			)
			err := v.Verify(t.Context(), test.m, nil)
			require.ErrorIs(t, err, test.want)
		})
	}
}
