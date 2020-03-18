// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/logging"
)

// Config contains all of the configurations of an Ava client.
type Config struct {
	// Networking configurations
	RemoteIP utils.IPDesc // Which Ava node to connect to

	// ID of the network that this client will be issuing transactions to
	NetworkID uint32

	// Transaction fee
	AvaTxFee uint64

	EnableCrypto  bool
	LoggingConfig logging.Config

	// Key describes which key to use to issue transactions
	// NumTxs describes the number of transactions to issue
	// MaxOutstandingTxs describes how many txs to pipeline
	Key, NumTxs, MaxOutstandingTxs int
	Chain                          ChainType
}
