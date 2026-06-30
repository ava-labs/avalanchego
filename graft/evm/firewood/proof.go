// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
)

// Prover is implemented by Firewood-backed [state.Database] accessors that can
// produce an eth_getProof-compatible proof at a given state root.
//
// accountKey must be keccak256(address) and each slotKeys entry must be
// keccak256(slot); both are 32-byte trie keys. The returned proof carries the
// account scalars plus RLP-encoded account-trie and storage-trie nodes
// verifiable with go-ethereum's trie.VerifyProof.
type Prover interface {
	EthGetProof(root common.Hash, accountKey []byte, slotKeys [][]byte) (*ffi.EthAccountProof, error)
}

var (
	_ Prover = (*stateAccessor)(nil)
	_ Prover = (*reconstructedStateAccessor)(nil)
)

// EthGetProof opens the committed revision at root and produces an
// eth_getProof-compatible proof for the account and the given storage slots.
func (s *stateAccessor) EthGetProof(root common.Hash, accountKey []byte, slotKeys [][]byte) (*ffi.EthAccountProof, error) {
	rev, err := s.triedb.Firewood.Revision(ffi.Hash(root))
	if err != nil {
		return nil, fmt.Errorf("opening revision %s: %w", root.Hex(), err)
	}
	defer func() {
		if dropErr := rev.Drop(); dropErr != nil {
			log.Warn("dropping revision after eth_getProof", "root", root.Hex(), "err", dropErr)
		}
	}()
	return rev.EthGetProof(accountKey, slotKeys)
}

// EthGetProof produces an eth_getProof-compatible proof against the
// reconstructed view, which must already be positioned at root.
func (s *reconstructedStateAccessor) EthGetProof(root common.Hash, accountKey []byte, slotKeys [][]byte) (*ffi.EthAccountProof, error) {
	if currRoot := common.Hash(s.recon.Root()); currRoot != root {
		return nil, fmt.Errorf("reconstructed view at root %s, requested %s", currRoot.Hex(), root.Hex())
	}
	return s.recon.EthGetProof(accountKey, slotKeys)
}
