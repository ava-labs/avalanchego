// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"context"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
)

// An AcceptedStateReceiver provides access to the accepted state ie. the state of the
// most recently accepted block.
type AcceptedStateReader interface {
	AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error)
	AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error)
}

// AcceptedContractCaller can be used to perform calls against the accepted state.
type AcceptedContractCaller interface {
	AcceptedCallContract(ctx context.Context, call ethereum.CallMsg) ([]byte, error)
}
