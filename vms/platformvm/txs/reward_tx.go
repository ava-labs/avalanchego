// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "github.com/ava-labs/avalanchego/ids"

// RewardTx is implemented by transactions that reward a staker
// (RewardValidatorTx and RewardAutoRenewedValidatorTx).
type RewardTx interface {
	StakerTxID() ids.ID
}
