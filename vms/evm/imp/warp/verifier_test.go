// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var _ Backend = (*backend)(nil)

type backend set.Set[ids.ID]

var errBlockNotAccepted = errors.New("block not accepted")

func (b backend) IsAcceptedBlock(_ context.Context, id ids.ID) error {
	if s := set.Set[ids.ID](b); !s.Contains(id) {
		return errBlockNotAccepted
	}
	return nil
}

// stubAddressedCallVerifier is an [AddressedCallVerifier] returning a fixed
// result for every addressed call.
type stubAddressedCallVerifier struct {
	err *common.AppError
}

func (v stubAddressedCallVerifier) VerifyAddressedCall(*payload.AddressedCall) *common.AppError {
	return v.err
}

func TestVerifier(t *testing.T) {
	addressedCallMsg, _ := newAddressedCall(t)
	hashMsg, hash := newHash(t)

	invalidPayloadMsg, err := warp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, nil)
	require.NoErrorf(t, err, "warp.NewUnsignedMessage(%s, %s, nil)", constants.UnitTestID, snowtest.XChainID)

	errRefused := &common.AppError{
		Code: 12345, // outside this package's reserved code range
	}

	tests := []struct {
		name           string
		acceptedBlocks set.Set[ids.ID]
		storage        *Storage
		addressedCall  AddressedCallVerifier
		m              *warp.UnsignedMessage
		want           *common.AppError
	}{
		{
			name:    "known_message",
			storage: NewStorage(memdb.New(), addressedCallMsg),
			m:       addressedCallMsg,
		},
		{
			name: "storage_error",
			storage: func() *Storage {
				db := memdb.New()
				require.NoErrorf(t, db.Close(), "%T.Close()", db)
				return NewStorage(db)
			}(),
			m: addressedCallMsg,
			want: &common.AppError{
				Code: StorageErrCode,
			},
		},
		{
			name:    "invalid_payload",
			storage: NewStorage(memdb.New()),
			m:       invalidPayloadMsg,
			want: &common.AppError{
				Code: ParseErrCode,
			},
		},
		{
			name:    "unknown_message",
			storage: NewStorage(memdb.New()),
			m:       addressedCallMsg,
			want: &common.AppError{
				Code: UnknownMessageErrCode,
			},
		},
		{
			name:          "addressed_call_accepted_by_extension",
			storage:       NewStorage(memdb.New()),
			addressedCall: stubAddressedCallVerifier{},
			m:             addressedCallMsg,
		},
		{
			name:    "addressed_call_refused_by_extension",
			storage: NewStorage(memdb.New()),
			addressedCall: stubAddressedCallVerifier{
				err: errRefused,
			},
			m:    addressedCallMsg,
			want: errRefused,
		},
		{
			name:           "accepted_block",
			acceptedBlocks: set.Of(hash.Hash),
			storage:        NewStorage(memdb.New()),
			m:              hashMsg,
		},
		{
			name:    "unaccepted_block",
			storage: NewStorage(memdb.New()),
			m:       hashMsg,
			want: &common.AppError{
				Code: NotAcceptedErrCode,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := NewVerifier(
				backend(test.acceptedBlocks),
				test.storage,
				test.addressedCall,
			)
			err := v.Verify(t.Context(), test.m, nil)
			require.ErrorIsf(t, err, test.want, "%T.Verify(...)", v)
		})
	}
}
