// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

// BlockchainSharedMemory provides the API for a blockchain to interact with
// shared memory of another blockchain
type BlockchainSharedMemory struct {
	blockchainID ids.ID
	sm           *SharedMemory
}

// GetDatabase returns and locks the provided DB
func (bsm *BlockchainSharedMemory) GetDatabase(id ids.ID) database.Database {
	sharedID := bsm.sm.sharedID(id, bsm.blockchainID)
	return bsm.sm.GetDatabase(sharedID)
}

// ReleaseDatabase unlocks the provided DB
func (bsm *BlockchainSharedMemory) ReleaseDatabase(id ids.ID) {
	sharedID := bsm.sm.sharedID(id, bsm.blockchainID)
	bsm.sm.ReleaseDatabase(sharedID)
}
