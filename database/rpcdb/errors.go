// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"github.com/ava-labs/avalanchego/database"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var (
	errEnumToError = map[rpcdbpb.Error]error{
		rpcdbpb.Error_ERROR_CLOSED:    database.ErrClosed,
		rpcdbpb.Error_ERROR_NOT_FOUND: database.ErrNotFound,
	}
	errorToErrEnum = map[error]rpcdbpb.Error{
		database.ErrClosed:   rpcdbpb.Error_ERROR_CLOSED,
		database.ErrNotFound: rpcdbpb.Error_ERROR_NOT_FOUND,
	}
)

func errorToRPCError(err error) error {
	if _, ok := errorToErrEnum[err]; ok {
		return nil
	}
	return err
}
