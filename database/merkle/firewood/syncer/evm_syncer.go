// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"errors"

	"github.com/ava-labs/firewood-go/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker"
)

type CodeQueue interface {
	AddCode([]common.Hash) error
}

// NewEVM returns a [Syncer] that also queues the contract code referenced by
// every proof Firewood accepts.
func NewEVM(
	config Config,
	db *ffi.Database,
	codeQueue CodeQueue,
	targetRoot ids.ID,
	proofClient *peertracker.Client,
) (*Syncer, error) {
	return newSyncer(config, db, targetRoot, proofClient, func(proofBytes []byte) (retErr error) {
		proof, err := db.UnmarshalRangeProof(proofBytes)
		if err != nil {
			return err
		}
		defer func() {
			retErr = errors.Join(retErr, proof.Drop())
		}()

		hashes, err := proof.CodeHashes()
		if err != nil {
			return err
		}
		codeHashes := make([]common.Hash, len(hashes))
		for i, h := range hashes {
			codeHashes[i] = common.Hash(h)
		}
		return codeQueue.AddCode(codeHashes)
	})
}
