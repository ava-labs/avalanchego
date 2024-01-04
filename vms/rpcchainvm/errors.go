// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

var (
	errEnumToError = map[vmpb.Error]error{
		vmpb.Error_ERROR_CLOSED:                                   database.ErrClosed,
		vmpb.Error_ERROR_NOT_FOUND:                                database.ErrNotFound,
		vmpb.Error_ERROR_HEIGHT_INDEX_INCOMPLETE:                  block.ErrIndexIncomplete,
		vmpb.Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED:               block.ErrStateSyncableVMNotImplemented,
		vmpb.Error_ERROR_STATE_SYNC_BLOCK_BACKFILLING_NOT_ENABLED: block.ErrBlockBackfillingNotEnabled,
		vmpb.Error_ERROR_STATE_SYNC_STOP_BLOCK_BACKFILLING:        block.ErrStopBlockBackfilling,
	}
	errorToErrEnum = map[error]vmpb.Error{
		database.ErrClosed:                     vmpb.Error_ERROR_CLOSED,
		database.ErrNotFound:                   vmpb.Error_ERROR_NOT_FOUND,
		block.ErrIndexIncomplete:               vmpb.Error_ERROR_HEIGHT_INDEX_INCOMPLETE,
		block.ErrStateSyncableVMNotImplemented: vmpb.Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED,
		block.ErrBlockBackfillingNotEnabled:    vmpb.Error_ERROR_STATE_SYNC_BLOCK_BACKFILLING_NOT_ENABLED,
		block.ErrStopBlockBackfilling:          vmpb.Error_ERROR_STATE_SYNC_STOP_BLOCK_BACKFILLING,
	}
)

func errorToRPCError(err error) error {
	if _, ok := errorToErrEnum[err]; ok {
		return nil
	}
	return err
}
