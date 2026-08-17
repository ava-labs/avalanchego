// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package eth

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
)

// slot is a single storage entry: the raw 32-byte slot key and its value.
type slot struct {
	key common.Hash
	val common.Hash
}

// accountSpec describes an account to materialize into the test state.
type accountSpec struct {
	addr  common.Address
	slots []slot
}

// builtState is the result of materializing accountSpecs into a firewood
// database and an equivalent go-ethereum trie.
type builtState struct {
	db   *ffi.Database
	root ffi.Hash
	// accountRLP maps account hash -> the RLP we stored for that account.
	accountRLP map[common.Hash][]byte
}

// buildState materializes specs into both a libevm state trie (to compute the
// canonical Ethereum storage roots and account RLPs) and a firewood database
// (flat keccak-prefixed keys). It asserts the two roots agree, so the firewood
// proofs can be checked against go-ethereum's verifier.
func buildState(t *testing.T, specs []accountSpec) builtState {
	t.Helper()
	r := require.New(t)

	db := newFirewoodDB(t)
	tdb := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), triedb.HashDefaults)
	accountTrie, err := tdb.OpenTrie(types.EmptyRootHash)
	r.NoError(err)

	merged := trienode.NewMergedNodeSet()
	var batch []ffi.BatchOp
	accountRLP := make(map[common.Hash][]byte)

	for _, spec := range specs {
		accHash := crypto.Keccak256Hash(spec.addr[:])
		storageRoot := types.EmptyRootHash

		if len(spec.slots) > 0 {
			storageTrie, err := tdb.OpenStorageTrie(types.EmptyRootHash, spec.addr, types.EmptyRootHash, accountTrie)
			r.NoError(err)

			for _, s := range spec.slots {
				r.NoError(storageTrie.UpdateStorage(spec.addr, s.key[:], s.val[:]))

				// firewood stores storage at keccak(addr) ++ keccak(slotKey),
				// with the RLP-encoded value (matching the trie's leaf value).
				slotHash := crypto.Keccak256Hash(s.key[:])
				fwdKey := append(append([]byte{}, accHash[:]...), slotHash[:]...)
				encodedVal, err := rlp.EncodeToBytes(s.val[:])
				r.NoError(err)
				batch = append(batch, ffi.Put(fwdKey, encodedVal))
			}

			root, set, err := storageTrie.Commit(false)
			r.NoError(err)
			if set != nil {
				r.NoError(merged.Merge(set))
			}
			storageRoot = root
		}

		acc := &types.StateAccount{
			Nonce:    1,
			Balance:  uint256.NewInt(100),
			Root:     storageRoot,
			CodeHash: types.EmptyCodeHash[:],
		}
		r.NoError(accountTrie.UpdateAccount(spec.addr, acc))

		encodedAcc, err := rlp.EncodeToBytes(acc)
		r.NoError(err)
		accountRLP[accHash] = encodedAcc
		batch = append(batch, ffi.Put(accHash[:], encodedAcc))
	}

	ethRoot, set, err := accountTrie.Commit(true)
	r.NoError(err)
	if set != nil {
		r.NoError(merged.Merge(set))
	}
	// TrieDB().Update rejects ethRoot == EmptyRootHash, so skip the no-op
	// update for an empty-state spec (no callers hit this today, but it keeps
	// the helper usable for future empty-state cases).
	if ethRoot != types.EmptyRootHash {
		r.NoError(tdb.TrieDB().Update(ethRoot, types.EmptyRootHash, 0, merged, nil))
	}

	fwdRoot, err := db.Update(batch)
	r.NoError(err)
	r.Equal(ethRoot, common.Hash(fwdRoot), "firewood root must match go-ethereum root")

	return builtState{db: db, root: fwdRoot, accountRLP: accountRLP}
}

// verifyProof loads the proof nodes into an in-memory keccak-keyed database and
// runs go-ethereum's trie.VerifyProof, returning the proven value (nil for a
// valid exclusion proof).
func verifyProof(t *testing.T, root common.Hash, key []byte, nodes [][]byte) []byte {
	t.Helper()
	proofDB := memorydb.New()
	for _, node := range nodes {
		require.NoError(t, proofDB.Put(crypto.Keccak256(node), node))
	}
	value, err := trie.VerifyProof(root, key, proofDB)
	require.NoError(t, err)
	return value
}

// ethProver is satisfied by *ffi.Revision and *ffi.Reconstructed,
// which expose the same EthGetProof signature.
type ethProver interface {
	EthGetProof(accountKey []byte, slotKeys [][]byte) (*ffi.EthAccountProof, error)
}

// proversAtRoot returns the committed revision and an equivalent reconstructed
// view, both rooted at built.root. Each view is released via t.Cleanup.
func proversAtRoot(t *testing.T, built builtState) []ethProver {
	t.Helper()
	r := require.New(t)

	// 1. Committed revision on the database built.db already committed to.
	rev, err := built.db.Revision(built.root)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(rev.Drop()) })

	// 2. Reconstructed view whose state matches the committed revision:
	// reconstructing with an empty batch on top of rev applies no changes, so
	// the resulting view sits at the same root.
	recon, err := rev.Reconstruct(nil)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(recon.Drop()) })
	r.Equal(built.root, recon.Root(), "reconstructed root must match committed root")

	return []ethProver{rev, recon}
}

func TestEthGetProofAccountInclusionAndExclusion(t *testing.T) {
	present := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	other := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	absent := common.HexToAddress("0x00000000000000000000000000000000000000ff")

	built := buildState(t, []accountSpec{{addr: present}, {addr: other}})
	root := common.Hash(built.root)

	accHash := crypto.Keccak256Hash(present[:])
	absentHash := crypto.Keccak256Hash(absent[:])

	for _, prover := range proversAtRoot(t, built) {
		t.Run(fmt.Sprintf("%T", prover), func(t *testing.T) {
			r := require.New(t)

			// Inclusion: the account proof verifies to the stored account RLP, and the
			// returned scalars match.
			proof, err := prover.EthGetProof(accHash[:], nil)
			r.NoError(err)
			r.Empty(proof.StorageProof)

			value := verifyProof(t, root, accHash[:], proof.AccountProof)
			r.Equal(built.accountRLP[accHash], value)

			r.Equal(uint64(1), proof.Nonce)
			r.Equal(types.EmptyRootHash, common.Hash(proof.StorageHash))
			r.Equal(types.EmptyCodeHash, common.Hash(proof.CodeHash))

			// Exclusion: an absent account verifies to a nil value (valid proof of
			// absence) and reports the default account shape.
			absentProof, err := prover.EthGetProof(absentHash[:], nil)
			r.NoError(err)
			r.Nil(verifyProof(t, root, absentHash[:], absentProof.AccountProof))
			r.Equal(uint64(0), absentProof.Nonce)
			r.Equal(types.EmptyRootHash, common.Hash(absentProof.StorageHash))
		})
	}
}

func TestEthGetProofStorageInclusionAndExclusion(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000c3")
	slots := []slot{
		{key: common.HexToHash("0x01"), val: common.HexToHash("0x1111")},
		{key: common.HexToHash("0x02"), val: common.HexToHash("0x2222")},
		{key: common.HexToHash("0x03"), val: common.HexToHash("0x3333")},
	}
	absentSlotKey := common.HexToHash("0xdead")

	built := buildState(t, []accountSpec{{addr: addr, slots: slots}})
	root := common.Hash(built.root)

	accHash := crypto.Keccak256Hash(addr[:])

	// The storage trie keys are keccak(slotKey); request all present slots plus
	// one absent slot.
	slotHashes := make([][]byte, 0, len(slots)+1)
	for _, s := range slots {
		h := crypto.Keccak256Hash(s.key[:])
		slotHashes = append(slotHashes, h[:])
	}
	absentSlotHash := crypto.Keccak256Hash(absentSlotKey[:])
	slotHashes = append(slotHashes, absentSlotHash[:])

	for _, prover := range proversAtRoot(t, built) {
		t.Run(fmt.Sprintf("%T", prover), func(t *testing.T) {
			r := require.New(t)

			proof, err := prover.EthGetProof(accHash[:], slotHashes)
			r.NoError(err)
			r.Len(proof.StorageProof, len(slots)+1)

			// StorageHash must equal the canonical storage trie root.
			storageHash := common.Hash(proof.StorageHash)
			r.NotEqual(types.EmptyRootHash, storageHash)

			// The account proof still verifies against the revision root.
			r.Equal(built.accountRLP[accHash], verifyProof(t, root, accHash[:], proof.AccountProof))

			// Present slots: storage proof verifies to the RLP-encoded value.
			for i, s := range slots {
				entry := proof.StorageProof[i]
				slotHash := crypto.Keccak256Hash(s.key[:])
				r.Equal(slotHash[:], entry.Key[:])

				expectedVal, err := rlp.EncodeToBytes(s.val[:])
				r.NoError(err)
				r.Equal(expectedVal, entry.Value)
				r.Equal(expectedVal, verifyProof(t, storageHash, slotHash[:], entry.Proof))
			}

			// Absent slot: nil value and a valid exclusion proof.
			absentEntry := proof.StorageProof[len(slots)]
			r.Equal(absentSlotHash[:], absentEntry.Key[:])
			r.Nil(absentEntry.Value)
			r.Nil(verifyProof(t, storageHash, absentSlotHash[:], absentEntry.Proof))
		})
	}
}

// findSlotsSharingFirstNibble returns n storage slots whose trie keys
// (keccak256 of the slot key) all share the same first nibble, with distinct
// non-zero values. Firewood path-compresses such slots beneath a single
// account child, so the storage trie root is a real subtree (extension or
// branch) rather than one synthesized leaf — the multi-slot-under-one-child
// path that a trie with distinct first nibbles never reaches.
func findSlotsSharingFirstNibble(t *testing.T, n int) []slot {
	t.Helper()
	buckets := make(map[byte][]slot)
	for i := uint64(1); i < 1_000_000; i++ {
		var key, val common.Hash
		binary.BigEndian.PutUint64(key[24:], i)
		binary.BigEndian.PutUint64(val[24:], i)
		nibble := crypto.Keccak256(key[:])[0] >> 4
		buckets[nibble] = append(buckets[nibble], slot{key: key, val: val})
		if len(buckets[nibble]) == n {
			return buckets[nibble]
		}
	}
	t.Fatalf("no %d slot keys sharing a first nibble within search bound", n)
	return nil
}

// TestEthGetProofStorageSharedFirstNibble covers the storage path where two
// slots' trie keys collide in their first nibble. Firewood folds them under a
// single account child, so the storage root is a branch synthesized over a
// real subtree rather than a lone leaf. This is the path that returns a proof
// not rooted at StorageHash if the root isn't reconstructed correctly; here
// the real go-ethereum verifier runs against StorageHash, so a regression
// fails the test.
func TestEthGetProofStorageSharedFirstNibble(t *testing.T) {
	r := require.New(t)

	addr := common.HexToAddress("0x00000000000000000000000000000000000000d4")
	slots := findSlotsSharingFirstNibble(t, 2)

	// Sanity-check the construction: the two slot trie keys really do collide
	// in their first nibble.
	h0 := crypto.Keccak256(slots[0].key[:])
	h1 := crypto.Keccak256(slots[1].key[:])
	r.Equal(h0[0]>>4, h1[0]>>4, "slot trie keys must share their first nibble")

	built := buildState(t, []accountSpec{{addr: addr, slots: slots}})

	rev, err := built.db.Revision(built.root)
	r.NoError(err)
	defer func() { r.NoError(rev.Drop()) }()

	accHash := crypto.Keccak256Hash(addr[:])
	slotHashes := make([][]byte, len(slots))
	for i, s := range slots {
		h := crypto.Keccak256Hash(s.key[:])
		slotHashes[i] = h[:]
	}

	proof, err := rev.EthGetProof(accHash[:], slotHashes)
	r.NoError(err)
	r.Len(proof.StorageProof, len(slots))

	storageHash := common.Hash(proof.StorageHash)
	r.NotEqual(types.EmptyRootHash, storageHash)

	for i, s := range slots {
		entry := proof.StorageProof[i]
		slotHash := crypto.Keccak256Hash(s.key[:])
		r.Equal(slotHash[:], entry.Key[:])

		expectedVal, err := rlp.EncodeToBytes(s.val[:])
		r.NoError(err)
		r.Equal(expectedVal, entry.Value, "slot %d value must match", i)
		r.Equal(expectedVal, verifyProof(t, storageHash, slotHash[:], entry.Proof),
			"slot %d storage proof must verify against StorageHash", i)
	}
}
