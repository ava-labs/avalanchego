// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"github.com/ava-labs/avalanchego/database"
)

var (
	errCodeToError = map[uint32]error{
		1: database.ErrClosed,
		2: database.ErrNotFound,
	}
	errorToErrCode = map[error]uint32{
		database.ErrClosed:   1,
		database.ErrNotFound: 2,
	}
)

func errorToRPCError(err error) error {
	switch err {
	case database.ErrClosed, database.ErrNotFound:
		return nil
	default:
		return err
	}
}
