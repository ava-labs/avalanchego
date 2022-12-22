// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package fx

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type CaminoFx interface {
	// Recovers signers addresses from [verifies] credentials for [utx] transaction
	RecoverAddresses(utx secp256k1fx.UnsignedTx, verifies []verify.Verifiable) (set.Set[ids.ShortID], error)
}
