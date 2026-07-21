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

// VerifyProof verifies an eth_getProof result against the state root,
// requiring the proven account and storage values to match the proof's own
// claims. An absent account verifies as a proof of absence, whose claims MUST
// be zero.
func VerifyProof(tb testing.TB, root common.Hash, proof *gethclient.AccountResult) {
	tb.Helper()

	accountRLP := proveTrieValue(tb, root, crypto.Keccak256(proof.Address.Bytes()), proof.AccountProof)
	if len(accountRLP) == 0 {
		assert.Zero(tb, proof.Balance.Sign(), "balance claimed for proven-absent account")
		assert.Zero(tb, proof.Nonce, "nonce claimed for proven-absent account")
		assert.Empty(tb, proof.StorageProof, "storage proofs claimed for proven-absent account")
		return
	}

	var account types.StateAccount
	require.NoError(tb, rlp.DecodeBytes(accountRLP, &account), "decode proven account")

	assert.Zerof(tb, account.Balance.ToBig().Cmp(proof.Balance), "proven account balance: proven %d, claimed %s", account.Balance.ToBig(), proof.Balance)
	assert.Equal(tb, proof.CodeHash[:], account.CodeHash, "proven account code hash")
	assert.Equal(tb, proof.Nonce, account.Nonce, "proven account nonce")
	assert.Equal(tb, proof.StorageHash, account.Root, "proven account storage root")

	for _, sp := range proof.StorageProof {
		value := proveStorageValue(tb, account.Root, common.HexToHash(sp.Key), sp.Proof)
		assert.Zerof(tb, sp.Value.Cmp(value), "proven storage value: proven %d, claimed %s", value, sp.Value)
	}
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
