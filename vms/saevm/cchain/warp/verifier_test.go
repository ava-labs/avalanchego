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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var _ Backend = (*backend)(nil)

type backend set.Set[ids.ID]

func (b backend) IsAccepted(_ context.Context, id ids.ID) error {
	if s := set.Set[ids.ID](b); !s.Contains(id) {
		return database.ErrNotFound
	}
	return nil
}

func TestVerifier(t *testing.T) {
	addressedCallMsg, _ := newAddressedCall(t)
	hashMsg, hash := newHash(t)

	invalidPayloadMsg, err := warp.NewUnsignedMessage(constants.UnitTestID, sourceChainID, nil)
	require.NoError(t, err)

	tests := []struct {
		name             string
		acceptedBlocks   set.Set[ids.ID]
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
			name: "unknown_message",
			m:    addressedCallMsg,
			want: &common.AppError{
				Code: ParseErrCode,
			},
		},
		{
			name:           "accepted_block",
			acceptedBlocks: set.Of(hash.Hash),
			m:              hashMsg,
		},
		{
			name: "unaccepted_block",
			m:    hashMsg,
			want: &common.AppError{
				Code: NotAcceptedErrCode,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := NewVerifier(
				backend(test.acceptedBlocks),
				NewStorage(memdb.New(), test.acceptedMessages...),
			)
			err := v.Verify(t.Context(), test.m, nil)
			require.ErrorIs(t, err, test.want)
		})
	}
}
