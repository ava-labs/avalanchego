// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

var (
	errEnumToError = map[vmpb.Error]error{
		vmpb.Error_ERROR_CLOSED:                       database.ErrClosed,
		vmpb.Error_ERROR_NOT_FOUND:                    database.ErrNotFound,
		vmpb.Error_ERROR_HEIGHT_INDEX_NOT_IMPLEMENTED: block.ErrHeightIndexedVMNotImplemented,
		vmpb.Error_ERROR_HEIGHT_INDEX_INCOMPLETE:      block.ErrIndexIncomplete,
		vmpb.Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED:   block.ErrStateSyncableVMNotImplemented,
	}
	errorToErrEnum = map[error]vmpb.Error{
		database.ErrClosed:                     vmpb.Error_ERROR_CLOSED,
		database.ErrNotFound:                   vmpb.Error_ERROR_NOT_FOUND,
		block.ErrHeightIndexedVMNotImplemented: vmpb.Error_ERROR_HEIGHT_INDEX_NOT_IMPLEMENTED,
		block.ErrIndexIncomplete:               vmpb.Error_ERROR_HEIGHT_INDEX_INCOMPLETE,
		block.ErrStateSyncableVMNotImplemented: vmpb.Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED,
	}
)

func errorToRPCError(err error) error {
	if _, ok := errorToErrEnum[err]; ok {
		return nil
	}
	return err
}
