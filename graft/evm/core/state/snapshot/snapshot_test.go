// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package snapshot

import (
	crand "crypto/rand"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/holiman/uint256"
)

// randomHash generates a random blob of data and returns it as a hash.
func randomHash() common.Hash {
	var hash common.Hash
	if n, err := crand.Read(hash[:]); n != common.HashLength || err != nil {
		panic(err)
	}
	return hash
}

// randomAccount generates a random account and returns it RLP encoded.
func randomAccount() []byte {
	a := &types.StateAccount{
		Balance:  uint256.NewInt(rand.Uint64()),
		Nonce:    rand.Uint64(),
		Root:     randomHash(),
		CodeHash: types.EmptyCodeHash[:],
	}
	data, _ := rlp.EncodeToBytes(a)
	return data
}

// randomAccountSet generates a set of random accounts with the given strings as
// the account address hashes.
func randomAccountSet(hashes ...string) map[common.Hash][]byte {
	accounts := make(map[common.Hash][]byte)
	for _, hash := range hashes {
		accounts[common.HexToHash(hash)] = randomAccount()
	}
	return accounts
}

// randomStorageSet generates a set of random slots with the given strings as
// the slot addresses.
func randomStorageSet(accounts []string, hashes [][]string, nilStorage [][]string) map[common.Hash]map[common.Hash][]byte {
	storages := make(map[common.Hash]map[common.Hash][]byte)
	for index, account := range accounts {
		storages[common.HexToHash(account)] = make(map[common.Hash][]byte)

		if index < len(hashes) {
			hashes := hashes[index]
			for _, hash := range hashes {
				storages[common.HexToHash(account)][common.HexToHash(hash)] = randomHash().Bytes()
			}
		}
		if index < len(nilStorage) {
			nils := nilStorage[index]
			for _, hash := range nils {
				storages[common.HexToHash(account)][common.HexToHash(hash)] = nil
			}
		}
	}
	return storages
}

// Tests that if a disk layer becomes stale, no active external references will
// be returned with junk data. This version of the test flattens every diff layer
// to check internal corner case around the bottom-most memory accumulator.
func TestDiskLayerExternalInvalidationFullFlatten(t *testing.T) {
	// Create an empty base layer and a snapshot tree out of it
	snaps := NewTestTree(rawdb.NewMemoryDatabase(), common.HexToHash("0x01"), common.HexToHash("0xff01"))
	// Retrieve a reference to the base and commit a diff on top
	ref := snaps.Snapshot(common.HexToHash("0xff01"))

	accounts := map[common.Hash][]byte{
		common.HexToHash("0xa1"): randomAccount(),
	}
	if err := snaps.UpdateWithBlockHashes(common.HexToHash("0x02"), common.HexToHash("0xff02"), common.HexToHash("0x01"), nil, accounts, nil); err != nil {
		t.Fatalf("failed to create a diff layer: %v", err)
	}
	if n := snaps.NumStateLayers(); n != 2 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 2)
	}
	if n := snaps.NumBlockLayers(); n != 2 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 2)
	}
	// Commit the diff layer onto the disk and ensure it's persisted
	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(common.HexToHash("0x02")); err != nil {
		t.Fatalf("failed to merge diff layer onto disk: %v", err)
	}
	// Since the base layer was modified, ensure that data retrievals on the external reference fail
	if acc, err := ref.Account(common.HexToHash("0x01")); err != ErrSnapshotStale {
		t.Errorf("stale reference returned account: %v (err: %v)", acc, err)
	}
	if slot, err := ref.Storage(common.HexToHash("0xa1"), common.HexToHash("0xb1")); err != ErrSnapshotStale {
		t.Errorf("stale reference returned storage slot: %v (err: %v)", slot, err)
	}
	if n := snaps.NumStateLayers(); n != 1 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 1)
	}
	if n := snaps.NumBlockLayers(); n != 1 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 1)
	}
}

// Tests that if a disk layer becomes stale, no active external references will
// be returned with junk data. This version of the test retains the bottom diff
// layer to check the usual mode of operation where the accumulator is retained.
func TestDiskLayerExternalInvalidationPartialFlatten(t *testing.T) {
	// Create an empty base layer and a snapshot tree out of it
	snaps := NewTestTree(rawdb.NewMemoryDatabase(), common.HexToHash("0x01"), common.HexToHash("0xff01"))
	// Retrieve a reference to the base and commit two diffs on top
	ref := snaps.Snapshot(common.HexToHash("0xff01"))

	accounts := map[common.Hash][]byte{
		common.HexToHash("0xa1"): randomAccount(),
	}
	if err := snaps.UpdateWithBlockHashes(common.HexToHash("0x02"), common.HexToHash("0xff02"), common.HexToHash("0x01"), nil, accounts, nil); err != nil {
		t.Fatalf("failed to create a diff layer: %v", err)
	}
	if err := snaps.UpdateWithBlockHashes(common.HexToHash("0x03"), common.HexToHash("0xff03"), common.HexToHash("0x02"), nil, accounts, nil); err != nil {
		t.Fatalf("failed to create a diff layer: %v", err)
	}
	if n := snaps.NumBlockLayers(); n != 3 {
		t.Errorf("pre-cap block layer count mismatch: have %d, want %d", n, 3)
	}
	if n := snaps.NumStateLayers(); n != 3 {
		t.Errorf("pre-cap state layer count mismatch: have %d, want %d", n, 3)
	}
	// Commit the diff layer onto the disk and ensure it's persisted
	defer func(memcap uint64) { aggregatorMemoryLimit = memcap }(aggregatorMemoryLimit)
	aggregatorMemoryLimit = 0

	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(common.HexToHash("0x02")); err != nil {
		t.Fatalf("Failed to flatten diff layer onto disk: %v", err)
	}
	// Since the base layer was modified, ensure that data retrieval on the external reference fail
	if acc, err := ref.Account(common.HexToHash("0x01")); err != ErrSnapshotStale {
		t.Errorf("stale reference returned account: %v (err: %v)", acc, err)
	}
	if slot, err := ref.Storage(common.HexToHash("0xa1"), common.HexToHash("0xb1")); err != ErrSnapshotStale {
		t.Errorf("stale reference returned storage slot: %#x (err: %v)", slot, err)
	}
	if n := snaps.NumBlockLayers(); n != 2 {
		t.Errorf("pre-cap block layer count mismatch: have %d, want %d", n, 2)
	}
	if n := snaps.NumStateLayers(); n != 2 {
		t.Errorf("pre-cap state layer count mismatch: have %d, want %d", n, 2)
	}
}

// Tests that if a diff layer becomes stale, no active external references will
// be returned with junk data. This version of the test retains the bottom diff
// layer to check the usual mode of operation where the accumulator is retained.
func TestDiffLayerExternalInvalidationPartialFlatten(t *testing.T) {
	// Un-commenting this triggers the bloom set to be deterministic. The values below
	// were used to trigger the flaw described in https://github.com/ethereum/go-ethereum/issues/27254.
	// bloomDestructHasherOffset, bloomAccountHasherOffset, bloomStorageHasherOffset = 14, 24, 5

	// Create an empty base layer and a snapshot tree out of it
	snaps := NewTestTree(rawdb.NewMemoryDatabase(), common.HexToHash("0x01"), common.HexToHash("0xff01"))
	// Commit three diffs on top and retrieve a reference to the bottommost
	accounts := map[common.Hash][]byte{
		common.HexToHash("0xa1"): randomAccount(),
	}
	if err := snaps.UpdateWithBlockHashes(common.HexToHash("0x02"), common.HexToHash("0xff02"), common.HexToHash("0x01"), nil, accounts, nil); err != nil {
		t.Fatalf("failed to create a diff layer: %v", err)
	}
	if err := snaps.UpdateWithBlockHashes(common.HexToHash("0x03"), common.HexToHash("0xff03"), common.HexToHash("0x02"), nil, accounts, nil); err != nil {
		t.Fatalf("failed to create a diff layer: %v", err)
	}
	if err := snaps.UpdateWithBlockHashes(common.HexToHash("0x04"), common.HexToHash("0xff04"), common.HexToHash("0x03"), nil, accounts, nil); err != nil {
		t.Fatalf("failed to create a diff layer: %v", err)
	}
	if n := snaps.NumStateLayers(); n != 4 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 4)
	}
	if n := snaps.NumBlockLayers(); n != 4 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 4)
	}
	ref := snaps.Snapshot(common.HexToHash("0xff02"))

	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(common.HexToHash("0x02")); err != nil {
		t.Fatal(err)
	}
	// Since the accumulator diff layer was modified, ensure that data retrievals on the external reference fail
	if acc, err := ref.Account(common.HexToHash("0x01")); err != ErrSnapshotStale {
		t.Errorf("stale reference returned account: %v (err: %v)", acc, err)
	}
	if slot, err := ref.Storage(common.HexToHash("0xa1"), common.HexToHash("0xb1")); err != ErrSnapshotStale {
		t.Errorf("stale reference returned storage slot: %#x (err: %v)", slot, err)
	}
	if n := snaps.NumStateLayers(); n != 3 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 3)
	}
	if n := snaps.NumBlockLayers(); n != 3 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 3)
	}
}

// TestPostFlattenBasicDataAccess tests some functionality regarding capping/flattening.
func TestPostFlattenBasicDataAccess(t *testing.T) {
	// setAccount is a helper to construct a random account entry and assign it to
	// an account slot in a snapshot
	setAccount := func(accKey string) map[common.Hash][]byte {
		return map[common.Hash][]byte{
			common.HexToHash(accKey): randomAccount(),
		}
	}
	// Create a starting base layer and a snapshot tree out of it
	snaps := NewTestTree(rawdb.NewMemoryDatabase(), common.HexToHash("0x01"), common.HexToHash("0xff01"))
	// The lowest difflayer
	snaps.UpdateWithBlockHashes(common.HexToHash("0xa1"), common.HexToHash("0xffa1"), common.HexToHash("0x01"), nil, setAccount("0xa1"), nil)
	snaps.UpdateWithBlockHashes(common.HexToHash("0xa2"), common.HexToHash("0xffa2"), common.HexToHash("0xa1"), nil, setAccount("0xa2"), nil)
	snaps.UpdateWithBlockHashes(common.HexToHash("0xb2"), common.HexToHash("0xffb2"), common.HexToHash("0xa1"), nil, setAccount("0xb2"), nil)

	snaps.UpdateWithBlockHashes(common.HexToHash("0xa3"), common.HexToHash("0xffa3"), common.HexToHash("0xa2"), nil, setAccount("0xa3"), nil)
	snaps.UpdateWithBlockHashes(common.HexToHash("0xb3"), common.HexToHash("0xffb3"), common.HexToHash("0xb2"), nil, setAccount("0xb3"), nil)

	// checkExist verifies if an account exists in a snapshot
	checkExist := func(layer Snapshot, key string) error {
		if data, _ := layer.Account(common.HexToHash(key)); data == nil {
			return fmt.Errorf("expected %x to exist, got nil", common.HexToHash(key))
		}
		return nil
	}
	checkNotExist := func(layer Snapshot, key string) error {
		if data, err := layer.Account(common.HexToHash(key)); err != nil {
			return fmt.Errorf("expected %x to not error: %w", key, err)
		} else if data != nil {
			return fmt.Errorf("expected %x to be empty, got %v", key, data)
		}
		return nil
	}
	// shouldErr checks that an account access errors as expected
	shouldErr := func(layer Snapshot, key string) error {
		if data, err := layer.Account(common.HexToHash(key)); err == nil {
			return fmt.Errorf("expected error, got data %v", data)
		}
		return nil
	}
	// Check basics for both snapshots 0xffa3 and 0xffb3
	snap := snaps.Snapshot(common.HexToHash("0xffa3")).(*diffLayer)

	// Check that the accounts that should exist are present in the snapshot
	if err := checkExist(snap, "0xa1"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xa2"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xa1"); err != nil {
		t.Error(err)
	}
	// Check that the accounts that should only exist in the other branch
	// of the snapshot tree are not present in the snapshot.
	if err := checkNotExist(snap, "0xb1"); err != nil {
		t.Error(err)
	}
	if err := checkNotExist(snap, "0xb2"); err != nil {
		t.Error(err)
	}

	snap = snaps.Snapshot(common.HexToHash("0xffb3")).(*diffLayer)

	// Check that the accounts that should exist are present in the snapshot
	if err := checkExist(snap, "0xa1"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xb2"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xb3"); err != nil {
		t.Error(err)
	}
	// Check that the accounts that should only exist in the other branch
	// of the snapshot tree are not present in the snapshot.
	if err := checkNotExist(snap, "0xa2"); err != nil {
		t.Error(err)
	}

	// Flatten a non-existent block should fail
	if err := snaps.Flatten(common.HexToHash("0x1337")); err == nil {
		t.Errorf("expected error, got none")
	}

	// Now, merge the a-chain layer by layer
	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(common.HexToHash("0xa1")); err != nil {
		t.Error(err)
	}
	if err := snaps.Flatten(common.HexToHash("0xa2")); err != nil {
		t.Error(err)
	}

	// At this point, a2 got merged into a1. Thus, a1 is now modified, and as a1 is
	// the parent of b2, b2 should no longer be able to iterate into parent.

	// These should still be accessible since it doesn't require iteration into the
	// disk layer. However, it's also valid for these diffLayers to be marked as stale.
	if err := checkExist(snap, "0xb2"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xb3"); err != nil {
		t.Error(err)
	}
	// But 0xa1 would need iteration into the modified parent
	// and 0xa2 and 0xa3 should never be present since they are
	// on the other branch of the snapshot tree. These should strictly
	// error since they're not in the diff layers and will result in
	// traversing into the stale disk layer.
	if err := shouldErr(snap, "0xa1"); err != nil {
		t.Error(err)
	}
	if err := shouldErr(snap, "0xa2"); err != nil {
		t.Error(err)
	}
	if err := shouldErr(snap, "0xa3"); err != nil {
		t.Error(err)
	}

	snap = snaps.Snapshot(common.HexToHash("0xffa3")).(*diffLayer)
	// Check that the accounts that should exist are present in the snapshot
	if err := checkExist(snap, "0xa1"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xa2"); err != nil {
		t.Error(err)
	}
	if err := checkExist(snap, "0xa3"); err != nil {
		t.Error(err)
	}
	// Check that the accounts that should only exist in the other branch
	// of the snapshot tree are not present in the snapshot.
	if err := checkNotExist(snap, "0xb1"); err != nil {
		t.Error(err)
	}
	if err := checkNotExist(snap, "0xb2"); err != nil {
		t.Error(err)
	}

	diskLayer := snaps.Snapshot(common.HexToHash("0xffa2"))
	// Check that the accounts that should exist are present in the snapshot
	if err := checkExist(diskLayer, "0xa1"); err != nil {
		t.Error(err)
	}
	if err := checkExist(diskLayer, "0xa2"); err != nil {
		t.Error(err)
	}
	// 0xa3 should not be included until the diffLayer built on top of
	// 0xffa2.
	if err := checkNotExist(diskLayer, "0xa3"); err != nil {
		t.Error(err)
	}
	// Check that the accounts that should only exist in the other branch
	// of the snapshot tree are not present in the snapshot.
	if err := checkNotExist(diskLayer, "0xb1"); err != nil {
		t.Error(err)
	}
	if err := checkNotExist(diskLayer, "0xb2"); err != nil {
		t.Error(err)
	}
}

// TestTreeFlattenDoesNotDropPendingLayers tests that Tree.Flatten correctly
// retains layers built on top of the given root layer in the presence of multiple
// different blocks inserted with an identical state root.
// In this example, (B, C) and (D, E) share the identical state root, but were
// inserted under different blocks.
//
//	  A
//	 /  \
//	B    C
//	|    |
//	D    E
//
// `t.Flatten(C)` should result in:
//
//	B    C
//	|    |
//	D    E
//
// With the branch D, E, hanging and relying on Discard to be called to
// garbage collect the references.
func TestTreeFlattenDoesNotDropPendingLayers(t *testing.T) {
	var (
		baseRoot      = common.HexToHash("0xffff01")
		baseBlockHash = common.HexToHash("0x01")
	)
	snaps := NewTestTree(rawdb.NewMemoryDatabase(), baseBlockHash, baseRoot)
	accounts := map[common.Hash][]byte{
		common.HexToHash("0xa1"): randomAccount(),
	}

	// Create N layers on top of base (N+1) total
	parentAHash := baseBlockHash
	parentBHash := baseBlockHash
	totalLayers := 10
	for i := 2; i <= totalLayers; i++ {
		diffBlockAHash := common.Hash{0xee, 0xee, byte(i)}
		diffBlockBHash := common.Hash{0xdd, 0xdd, byte(i)}
		diffBlockRoot := common.Hash{0xff, 0xff, byte(i)}
		if err := snaps.UpdateWithBlockHashes(diffBlockAHash, diffBlockRoot, parentAHash, nil, accounts, nil); err != nil {
			t.Fatalf("failed to create a diff layer: %v", err)
		}
		if err := snaps.UpdateWithBlockHashes(diffBlockBHash, diffBlockRoot, parentBHash, nil, accounts, nil); err != nil {
			t.Fatalf("failed to create a diff layer: %v", err)
		}

		parentAHash = diffBlockAHash
		parentBHash = diffBlockBHash
	}

	if n := snaps.NumStateLayers(); n != 10 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 10)
	}
	if n := snaps.NumBlockLayers(); n != 19 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 19)
	}

	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(common.Hash{0xee, 0xee, byte(2)}); err != nil {
		t.Fatalf("failed to flatten diff layer from chain A: %v", err)
	}

	if n := snaps.NumStateLayers(); n != 9 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 9)
	}
	if n := snaps.NumBlockLayers(); n != 18 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 18)
	}

	// Attempt to flatten from chain B should error
	if err := snaps.Flatten(common.Hash{0xdd, 0xdd, byte(2)}); err == nil {
		t.Errorf("expected flattening abandoned chain to error")
	}

	for i := 2; i <= 10; i++ {
		if err := snaps.Discard(common.Hash{0xdd, 0xdd, byte(i)}); err != nil {
			t.Errorf("attempt to discard snapshot from abandoned chain failed: %v", err)
		}
	}

	if n := snaps.NumStateLayers(); n != 9 {
		t.Errorf("pre-flatten state layer count mismatch: have %d, want %d", n, 9)
	}
	if n := snaps.NumBlockLayers(); n != 9 {
		t.Errorf("pre-flatten block layer count mismatch: have %d, want %d", n, 18)
	}
}

func TestStaleOriginLayer(t *testing.T) {
	var (
		baseRoot       = common.HexToHash("0xffff01")
		baseBlockHash  = common.HexToHash("0x01")
		diffRootA      = common.HexToHash("0xff02")
		diffBlockHashA = common.HexToHash("0x02")
		diffRootB      = common.HexToHash("0xff03")
		diffBlockHashB = common.HexToHash("0x03")
		diffRootC      = common.HexToHash("0xff04")
		diffBlockHashC = common.HexToHash("0x04")
	)
	snaps := NewTestTree(rawdb.NewMemoryDatabase(), baseBlockHash, baseRoot)
	addrA := randomHash()
	accountsA := map[common.Hash][]byte{
		addrA: randomAccount(),
	}
	addrB := randomHash()
	accountsB := map[common.Hash][]byte{
		addrB: randomAccount(),
	}
	addrC := randomHash()
	accountsC := map[common.Hash][]byte{
		addrC: randomAccount(),
	}

	// Create diff layer A containing account 0xa1
	if err := snaps.UpdateWithBlockHashes(diffBlockHashA, diffRootA, baseBlockHash, nil, accountsA, nil); err != nil {
		t.Errorf("failed to create diff layer A: %v", err)
	}
	// Flatten account 0xa1 to disk
	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(diffBlockHashA); err != nil {
		t.Errorf("failed to flatten diff block A: %v", err)
	}
	// Create diff layer B containing account 0xa2
	// The bloom filter should contain only 0xa2.
	if err := snaps.UpdateWithBlockHashes(diffBlockHashB, diffRootB, diffBlockHashA, nil, accountsB, nil); err != nil {
		t.Errorf("failed to create diff layer B: %v", err)
	}
	// Create diff layer C containing account 0xa3
	// The bloom filter should contain 0xa2 and 0xa3
	if err := snaps.UpdateWithBlockHashes(diffBlockHashC, diffRootC, diffBlockHashB, nil, accountsC, nil); err != nil {
		t.Errorf("failed to create diff layer C: %v", err)
	}

	if err := snaps.Flatten(diffBlockHashB); err != nil {
		t.Errorf("failed to flattten diff block A: %v", err)
	}
	snap := snaps.Snapshot(diffRootC)
	if _, err := snap.Account(addrA); err != nil {
		t.Errorf("expected account to exist: %v", err)
	}
}

func TestRebloomOnFlatten(t *testing.T) {
	// Create diff layers, including a level with two children, then flatten
	// the layers and assert that the bloom filters are being updated correctly.
	//
	// Each layer will add an account which will be in the blooms for every
	// following difflayer. No accesses would need to touch disk. Layers C and D
	// should have all addrs in their blooms except for each others.
	//
	// After flattening A into the root, the remaining blooms should no longer
	// have addrA but should retain all the others.
	//
	// After flattening B into A, blooms C and D should contain only their own
	// addrs.
	//
	//          Initial Root (diskLayer)
	//                   |
	//                   A <- First diffLayer. Adds addrA
	//                   |
	//                   B <- Second diffLayer. Adds addrB
	//                 /  \
	//  Adds addrC -> C    D <- Adds addrD

	var (
		baseRoot       = common.HexToHash("0xff01")
		baseBlockHash  = common.HexToHash("0x01")
		diffRootA      = common.HexToHash("0xff02")
		diffBlockHashA = common.HexToHash("0x02")
		diffRootB      = common.HexToHash("0xff03")
		diffBlockHashB = common.HexToHash("0x03")
		diffRootC      = common.HexToHash("0xff04")
		diffBlockHashC = common.HexToHash("0x04")
		diffRootD      = common.HexToHash("0xff05")
		diffBlockHashD = common.HexToHash("0x05")
	)

	snaps := NewTestTree(rawdb.NewMemoryDatabase(), baseBlockHash, baseRoot)
	addrA := randomHash()
	accountsA := map[common.Hash][]byte{
		addrA: randomAccount(),
	}
	addrB := randomHash()
	accountsB := map[common.Hash][]byte{
		addrB: randomAccount(),
	}
	addrC := randomHash()
	accountsC := map[common.Hash][]byte{
		addrC: randomAccount(),
	}
	addrD := randomHash()
	accountsD := map[common.Hash][]byte{
		addrD: randomAccount(),
	}

	// Build the tree
	if err := snaps.UpdateWithBlockHashes(diffBlockHashA, diffRootA, baseBlockHash, nil, accountsA, nil); err != nil {
		t.Errorf("failed to create diff layer A: %v", err)
	}
	if err := snaps.UpdateWithBlockHashes(diffBlockHashB, diffRootB, diffBlockHashA, nil, accountsB, nil); err != nil {
		t.Errorf("failed to create diff layer B: %v", err)
	}
	if err := snaps.UpdateWithBlockHashes(diffBlockHashC, diffRootC, diffBlockHashB, nil, accountsC, nil); err != nil {
		t.Errorf("failed to create diff layer C: %v", err)
	}
	if err := snaps.UpdateWithBlockHashes(diffBlockHashD, diffRootD, diffBlockHashB, nil, accountsD, nil); err != nil {
		t.Errorf("failed to create diff layer D: %v", err)
	}

	assertBlooms := func(snap Snapshot, hitsA, hitsB, hitsC, hitsD bool) {
		dl, ok := snap.(*diffLayer)
		if !ok {
			t.Fatal("snapshot should be a diffLayer")
		}

		if hitsA != dl.diffed.ContainsHash(accountBloomHash(addrA)) {
			t.Errorf("expected bloom filter to return %t but got %t", hitsA, !hitsA)
		}

		if hitsB != dl.diffed.ContainsHash(accountBloomHash(addrB)) {
			t.Errorf("expected bloom filter to return %t but got %t", hitsB, !hitsB)
		}

		if hitsC != dl.diffed.ContainsHash(accountBloomHash(addrC)) {
			t.Errorf("expected bloom filter to return %t but got %t", hitsC, !hitsC)
		}

		if hitsD != dl.diffed.ContainsHash(accountBloomHash(addrD)) {
			t.Errorf("expected bloom filter to return %t but got %t", hitsD, !hitsD)
		}
	}

	// First check that each layer's bloom has all current and ancestor addrs,
	// but no sibling-only addrs.
	assertBlooms(snaps.Snapshot(diffRootB), true, true, false, false)
	assertBlooms(snaps.Snapshot(diffRootC), true, true, true, false)
	assertBlooms(snaps.Snapshot(diffRootD), true, true, false, true)

	// Flatten diffLayer A, making it a disk layer and trigger a rebloom on B, C
	// and D. If we didn't rebloom, or didn't rebloom recursively, then blooms C
	// and D would still think addrA was in the diff layers
	snaps.verified = true // Bypass validation of junk data
	if err := snaps.Flatten(diffBlockHashA); err != nil {
		t.Errorf("failed to flattten diff block A: %v", err)
	}

	// Check that no blooms still have addrA, but they have all the others
	assertBlooms(snaps.Snapshot(diffRootB), false, true, false, false)
	assertBlooms(snaps.Snapshot(diffRootC), false, true, true, false)
	assertBlooms(snaps.Snapshot(diffRootD), false, true, false, true)

	// Flatten diffLayer B, making it a disk layer and trigger a rebloom on C
	// and D. If we didn't rebloom, or didn't rebloom recursively, then blooms C
	// and D would still think addrB was in the diff layers
	if err := snaps.Flatten(diffBlockHashB); err != nil {
		t.Errorf("failed to flattten diff block A: %v", err)
	}

	// Blooms C and D should now have only their own addrs
	assertBlooms(snaps.Snapshot(diffRootC), false, false, true, false)
	assertBlooms(snaps.Snapshot(diffRootD), false, false, false, true)
}

// TestReadStateDuringFlattening tests the scenario that, during the
// bottom diff layers are merging which tags these as stale, the read
// happens via a pre-created top snapshot layer which tries to access
// the state in these stale layers. Ensure this read can retrieve the
// right state back(block until the flattening is finished) instead of
// an unexpected error(snapshot layer is stale).
func TestReadStateDuringFlattening(t *testing.T) {
	// setAccount is a helper to construct a random account entry and assign it to
	// an account slot in a snapshot
	setAccount := func(accKey string) map[common.Hash][]byte {
		return map[common.Hash][]byte{
			common.HexToHash(accKey): randomAccount(),
		}
	}

	var (
		baseRoot       = common.HexToHash("0xff01")
		baseBlockHash  = common.HexToHash("0x01")
		diffRootA      = common.HexToHash("0xff02")
		diffBlockHashA = common.HexToHash("0x02")
		diffRootB      = common.HexToHash("0xff03")
		diffBlockHashB = common.HexToHash("0x03")
		diffRootC      = common.HexToHash("0xff04")
		diffBlockHashC = common.HexToHash("0x04")
	)

	snaps := NewTestTree(rawdb.NewMemoryDatabase(), baseBlockHash, baseRoot)

	// 4 layers in total, 3 diff layers and 1 disk layers
	snaps.UpdateWithBlockHashes(diffBlockHashA, diffRootA, baseBlockHash, nil, setAccount("0xa1"), nil)
	snaps.UpdateWithBlockHashes(diffBlockHashB, diffRootB, diffBlockHashA, nil, setAccount("0xa2"), nil)
	snaps.UpdateWithBlockHashes(diffBlockHashC, diffRootC, diffBlockHashB, nil, setAccount("0xa3"), nil)

	// Obtain the topmost snapshot handler for state accessing
	snap := snaps.Snapshot(diffRootC)

	// Register the testing hook to access the state after flattening
	var result = make(chan *types.SlimAccount)
	snaps.onFlatten = func() {
		// Spin up a thread to read the account from the pre-created
		// snapshot handler. It's expected to be blocked.
		go func() {
			account, _ := snap.Account(common.HexToHash("0xa1"))
			result <- account
		}()
		select {
		case res := <-result:
			t.Fatalf("Unexpected return %v", res)
		case <-time.NewTimer(time.Millisecond * 300).C:
		}
	}
	// Flatten the first diff block, which will mark the bottom-most layer as stale.
	snaps.Flatten(diffBlockHashA)
	select {
	case account := <-result:
		if account == nil {
			t.Fatal("Failed to retrieve account")
		}
	case <-time.NewTimer(time.Millisecond * 300).C:
		t.Fatal("Unexpected blocker")
	}
}
