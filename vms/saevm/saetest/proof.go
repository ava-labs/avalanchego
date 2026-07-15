// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient/gethclient"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// VerifyProof asserts that proof's reported account and storage values are
// Merkle-provable against root.
func VerifyProof(tb testing.TB, root common.Hash, proof *gethclient.AccountResult) {
	tb.Helper()

	account := proveAccount(tb, root, proof.Address, proof.AccountProof)
	assert.Zerof(tb, account.Balance.ToBig().Cmp(proof.Balance), "proven account balance: proven %d, claimed %s", account.Balance.ToBig(), proof.Balance)
	assert.Equal(tb, proof.CodeHash[:], account.CodeHash, "proven account code hash")
	assert.Equal(tb, proof.Nonce, account.Nonce, "proven account nonce")
	assert.Equal(tb, proof.StorageHash, account.Root, "proven account storage root")

	for _, sp := range proof.StorageProof {
		// An empty storage trie has no nodes, so every slot is zero by exclusion.
		if account.Root == types.EmptyRootHash {
			assert.Zerof(tb, sp.Value.Sign(), "storage slot %s in empty trie must be zero, got %s", sp.Key, sp.Value)
			continue
		}
		value := proveStorageValue(tb, account.Root, common.HexToHash(sp.Key), sp.Proof)
		assert.Zerof(tb, sp.Value.Cmp(value), "proven storage value: proven %d, claimed %s", value, sp.Value)
	}
}

func proveAccount(tb testing.TB, root common.Hash, addr common.Address, nodes []string) types.StateAccount {
	tb.Helper()

	accountRLP := proveTrieValue(tb, root, crypto.Keccak256(addr.Bytes()), nodes)
	var account types.StateAccount
	require.NoError(tb, rlp.DecodeBytes(accountRLP, &account), "decode proven account")
	return account
}

func proveStorageValue(tb testing.TB, root common.Hash, key common.Hash, nodes []string) *big.Int {
	tb.Helper()

	storageRLP := proveTrieValue(tb, root, crypto.Keccak256(key.Bytes()), nodes)
	var value big.Int
	require.NoError(tb, rlp.DecodeBytes(storageRLP, &value), "decode proven storage value")
	return &value
}

func proveTrieValue(tb testing.TB, root common.Hash, key []byte, nodes []string) []byte {
	tb.Helper()

	proofDB := memorydb.New()
	for _, nodeStr := range nodes {
		nodeBytes := common.FromHex(nodeStr)
		nodeHash := crypto.Keccak256(nodeBytes)
		require.NoErrorf(tb, proofDB.Put(nodeHash, nodeBytes), "%T.Put(proof node)", proofDB)
	}

	value, err := trie.VerifyProof(root, key, proofDB)
	require.NoError(tb, err, "VerifyProof()")
	return value
}
