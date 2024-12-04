// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethsync

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

type (
	ChangeProof = merkledb.ChangeProof
	RangeProof  = merkledb.RangeProof
	Key         = merkledb.Key
	ProofNode   = merkledb.ProofNode
	KeyValue    = merkledb.KeyValue
)

var (
	ToKey                          = merkledb.ToKey
	ErrStartAfterEnd               = merkledb.ErrStartAfterEnd
	ErrEmptyProof                  = merkledb.ErrEmptyProof
	ErrUnexpectedEndProof          = merkledb.ErrUnexpectedEndProof
	ErrNoStartProof                = merkledb.ErrNoStartProof
	ErrNoEndProof                  = merkledb.ErrNoEndProof
	ErrProofNodeHasUnincludedValue = merkledb.ErrProofNodeHasUnincludedValue
	ErrProofValueDoesntMatch       = merkledb.ErrProofValueDoesntMatch
	ErrPartialByteLengthWithValue  = merkledb.ErrPartialByteLengthWithValue
	ErrProofNodeNotForKey          = merkledb.ErrProofNodeNotForKey
	ErrNonIncreasingProofNodes     = merkledb.ErrNonIncreasingProofNodes
	ErrNonIncreasingValues         = merkledb.ErrNonIncreasingValues
	ErrStateFromOutsideOfRange     = merkledb.ErrStateFromOutsideOfRange
)

const (
	HashLength = merkledb.HashLength
)

var rootKey = []byte{}

type manager interface {
	EnqueueWork(start, end maybe.Maybe[[]byte], priorityAsByte byte)
}

type db struct {
	db         ethdb.Database
	triedb     *triedb.Database
	root       common.Hash
	layerID    common.Hash
	updateLock sync.RWMutex
	lastRoots  map[common.Hash]common.Hash
	manager    manager
}

func New(ctx context.Context, disk database.Database, config merkledb.Config) (*db, error) {
	ethdb := rawdb.NewDatabase(evm.Database{Database: disk})
	root := types.EmptyRootHash
	diskRoot, err := ethdb.Get(rootKey)
	if err == nil {
		copy(root[:], diskRoot)
	} else if err != database.ErrNotFound {
		return nil, err
	}

	triedb := triedb.NewDatabase(ethdb, &triedb.Config{PathDB: pathdb.Defaults})
	return &db{
		db:        ethdb,
		triedb:    triedb,
		root:      root,
		layerID:   root,
		lastRoots: make(map[common.Hash]common.Hash),
	}, nil
}

func (db *db) KVCallback(start, end maybe.Maybe[[]byte], priority byte, stateID ids.ID, keyValues []merkledb.KeyChange) error {
	if len(start.Value()) == 64 {
		return nil // Storage trie, ignore
	}
	for _, kv := range keyValues {
		acc := new(types.StateAccount)
		if err := rlp.DecodeBytes(kv.Value.Value(), acc); err != nil {
			continue // failed to decode account
		}
		if acc.Root == types.EmptyRootHash {
			continue // empty account
		}

		if db.manager != nil {
			var start, end []byte
			start = append(start, kv.Key...)
			start = append(start, bytes.Repeat([]byte{0}, 32)...)
			end = append(end, kv.Key...)
			end = append(end, bytes.Repeat([]byte{0xff}, 32)...)
			db.manager.EnqueueWork(maybe.Some(start), maybe.Some(end), priority)
		}
	}
	return nil
}

func (db *db) Clear() error {
	return database.Clear(Database{db.db}, ethdb.IdealBatchSize)
}

func (db *db) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	db.updateLock.RLock()
	defer db.updateLock.RUnlock()

	if db.root == types.EmptyRootHash {
		return ids.ID{}, nil
	}
	return ids.ID(db.root), nil
}

func (db *db) GetRangeProofAtRoot(ctx context.Context, rootID ids.ID, start, end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	stateRoot := common.BytesToHash(rootID[:])
	tr, err := trie.New(trie.StateTrieID(stateRoot), db.triedb)
	if err != nil {
		return nil, err
	}
	var additionalProof proof
	if len(start.Value()) == 64 {
		// This is a storage trie
		accHash := start.Value()[:32]
		accBytes, err := tr.Get(accHash)
		if err != nil {
			return nil, err
		}
		acc := new(types.StateAccount)
		if err := rlp.DecodeBytes(accBytes, acc); err != nil {
			return nil, err
		}

		// TODO: to prove the account root, we include this for now.
		// The client can find a better way to track this.
		if err := tr.Prove(accHash, &additionalProof); err != nil {
			return nil, err
		}

		tr, err = trie.New(trie.StorageTrieID(stateRoot, common.BytesToHash(accHash), acc.Root), db.triedb)
		if err != nil {
			return nil, err
		}

		start = maybe.Some(start.Value()[32:])
		if end.HasValue() {
			if len(end.Value()) != 64 {
				return nil, fmt.Errorf("invalid end key length: %d", len(end.Value()))
			}
			if !bytes.Equal(end.Value()[:32], accHash) {
				return nil, fmt.Errorf("end key does not match account hash: %x != %x", end.Value()[:32], accHash)
			}
			end = maybe.Some(end.Value()[32:])
		}
	}
	response, err := db.getRangeProofAtRoot(ctx, tr, rootID, start, end, maxLength)
	if err != nil {
		return nil, err
	}
	response.StartProof = append(response.StartProof, additionalProof...)
	return response, nil
}

func (db *db) getRangeProofAtRoot(
	ctx context.Context, tr *trie.Trie,
	rootID ids.ID, start, end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	response := &RangeProof{}
	nodeIt, err := tr.NodeIterator(start.Value())
	if err != nil {
		return nil, err
	}
	it := trie.NewIterator(nodeIt)
	for it.Next() {
		if end.HasValue() && bytes.Compare(it.Key, end.Value()) > 0 {
			break
		}
		if len(response.KeyValues) >= maxLength {
			break
		}

		response.KeyValues = append(response.KeyValues, KeyValue{
			Key:   bytes.Clone(it.Key),
			Value: bytes.Clone(it.Value),
		})
	}
	if err := it.Err; err != nil {
		return nil, err
	}
	for i, it := range response.KeyValues {
		if len(it.Key) == 0 {
			panic("empty key at index " + fmt.Sprintf("%d", i))
		}
		if len(it.Value) == 0 {
			panic("empty value")
		}
		// fmt.Println("--> kv", i, hex.EncodeToString(it.Key), hex.EncodeToString(it.Value))
	}

	startKey := start.Value()
	if len(startKey) == 0 && len(response.KeyValues) > 0 {
		startKey = bytes.Repeat([]byte{0}, 32) // XXX
	}
	if err := tr.Prove(startKey, (*proof)(&response.StartProof)); err != nil {
		return nil, err
	}
	kvs := response.KeyValues
	if len(kvs) > 0 {
		// If there is a non-zero number of keys, set [end] for the range proof to the last key.
		end := kvs[len(kvs)-1].Key
		if err := tr.Prove(end, (*proof)(&response.EndProof)); err != nil {
			return nil, err
		}
	} else if end.HasValue() {
		// If there are no keys, and [end] is set, set [end] for the range proof to [end].
		if err := tr.Prove(end.Value(), (*proof)(&response.EndProof)); err != nil {
			return nil, err
		}
	}

	startLen := len(start.Value())
	if startLen != 0 && startLen != 32 {
		panic("invalid start key length")
	}

	fmt.Println("proof generated",
		"start", hex.EncodeToString(start.Value()),
		"end", hex.EncodeToString(end.Value()),
		"startProof", len(response.StartProof),
		"endProof", len(response.EndProof),
		"keyValues", len(response.KeyValues),
	)
	return response, nil
}

func (db *db) VerifyRangeProof(ctx context.Context, proof *RangeProof, start, end maybe.Maybe[[]byte], expectedRootID ids.ID) error {
	var prefix []byte
	verifyRootID := expectedRootID
	if len(start.Value()) == 64 {
		prefix = start.Value()[:32]
		start = maybe.Some(start.Value()[32:])
		if end.HasValue() {
			end = maybe.Some(end.Value()[32:])
		}

		// If the proof is for a storage trie, the expected root ID should be
		// recovered from the "additionalProof", included in StartProof.
		proofDB := rawdb.NewMemoryDatabase()
		for _, node := range proof.StartProof {
			if err := proofDB.Put(node.Key.Bytes(), node.ValueOrHash.Value()); err != nil {
				return err
			}
		}
		val, err := trie.VerifyProof(
			common.BytesToHash(expectedRootID[:]),
			prefix,
			proofDB,
		)
		if err != nil {
			return fmt.Errorf("failed to verify proof: %w", err)
		}
		account := new(types.StateAccount)
		if err := rlp.DecodeBytes(val, account); err != nil {
			return fmt.Errorf("failed to decode account: %w", err)
		}

		verifyRootID = ids.ID(account.Root)
	}
	return db.verifyRangeProof(ctx, prefix, proof, start, end, verifyRootID)
}

func (db *db) verifyRangeProof(ctx context.Context, prefix []byte, proof *RangeProof, start, end maybe.Maybe[[]byte], expectedRootID ids.ID) error {
	fmt.Println(
		"proof verification",
		"start", hex.EncodeToString(start.Value()),
		"end", hex.EncodeToString(end.Value()),
		"expectedRootID", expectedRootID,
		"kvs", len(proof.KeyValues),
	)

	proofDB := rawdb.NewMemoryDatabase()
	for _, node := range proof.StartProof {
		if err := proofDB.Put(node.Key.Bytes(), node.ValueOrHash.Value()); err != nil {
			return err
		}
	}
	for _, node := range proof.EndProof {
		if err := proofDB.Put(node.Key.Bytes(), node.ValueOrHash.Value()); err != nil {
			return err
		}
	}
	keys := make([][]byte, 0, len(proof.KeyValues))
	vals := make([][]byte, 0, len(proof.KeyValues))
	for i := range proof.KeyValues {
		if len(proof.KeyValues[i].Key) == 0 || len(proof.KeyValues[i].Value) == 0 {
			return fmt.Errorf("invalid key-value pair at index %d", i)
		}
		keys = append(keys, proof.KeyValues[i].Key)
		vals = append(vals, proof.KeyValues[i].Value)
	}
	root := common.BytesToHash(expectedRootID[:])

	startKey := start.Value()
	if len(startKey) == 0 && len(keys) > 0 {
		startKey = bytes.Repeat([]byte{0}, 32) // XXX
	}
	if len(keys) == 0 && end.HasValue() {
		if err := trie.VerifyRangeProofEmpty(root, startKey, end.Value(), proofDB); err != nil {
			fmt.Println("proof verification failed empty", err)
			return err
		}
	} else {
		if _, err := trie.VerifyRangeProof(root, startKey, keys, vals, proofDB); err != nil {
			fmt.Println("proof verification failed", err)
			return err
		}
	}
	fmt.Println("proof verified")
	return nil
}

func (db *db) CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) error {
	var prefix []byte
	if len(start.Value()) == 64 {
		prefix = start.Value()[:32]
		start = maybe.Some(start.Value()[32:])
		if end.HasValue() {
			end = maybe.Some(end.Value()[32:])
		}
	}
	return db.commitRangeProof(ctx, prefix, start, end, proof)
}

func (db *db) commitRangeProof(ctx context.Context, prefix []byte, start, end maybe.Maybe[[]byte], proof *RangeProof) error {
	kvs := make(map[string][]byte)
	if err := db.getKVs(prefix, start.Value(), end.Value(), kvs); err != nil {
		return err
	}
	deletes := make([]KeyValue, 0, len(kvs))
	for k := range kvs {
		deletes = append(deletes, KeyValue{
			Key:   []byte(k),
			Value: nil, // Delete
		})
	}
	if len(deletes) > 0 {
		fmt.Println("deleting", len(deletes), "keys")
		if err := db.updateKVs(prefix, deletes); err != nil {
			return err
		}
	}

	return db.updateKVs(prefix, proof.KeyValues)
}

func (db *db) NextKey(lastReceived []byte, rangeEnd maybe.Maybe[[]byte]) ([]byte, error) {
	fmt.Println(
		"next key",
		"lastReceived", hex.EncodeToString(lastReceived),
		"rangeEnd", hex.EncodeToString(rangeEnd.Value()),
	)
	if len(rangeEnd.Value()) == 64 {
		prefix := rangeEnd.Value()[:32]
		next, err := db.NextKey(lastReceived, maybe.Nothing[[]byte]())
		if err != nil {
			return nil, err
		}
		retval := make([]byte, 0, len(prefix)+len(next))
		retval = append(retval, prefix...)
		retval = append(retval, next...)
		fmt.Println("next key", hex.EncodeToString(retval))
		return retval, nil
	}
	if len(lastReceived) != 32 {
		return nil, fmt.Errorf("key length is not 32: %d", len(lastReceived))
	}
	keyCopy := bytes.Clone(lastReceived)
	IncrOne(keyCopy)
	return keyCopy, nil
}

func (db *db) updateKVs(prefix []byte, kvs []merkledb.KeyValue) error {
	db.updateLock.Lock()
	defer db.updateLock.Unlock()

	fmt.Println(
		"updating",
		"prefix", hex.EncodeToString(prefix),
		"kvs", len(kvs),
	)

	trID := db.getTrieID(prefix)
	tr, err := trie.New(trID, db.triedb)
	if err != nil {
		return err
	}
	for _, op := range kvs {
		if len(op.Value) == 0 {
			if err := tr.Delete(op.Key); err != nil {
				return err
			}
		} else {
			if err := tr.Update(op.Key, op.Value); err != nil {
				return err
			}
		}
	}
	root, nodeSet, err := tr.Commit(true)
	if err != nil {
		return err
	}
	fmt.Fprintln(
		os.Stderr,
		"committing", len(kvs),
		"parent", hex.EncodeToString(db.layerID[:]),
		"root", hex.EncodeToString(root[:]),
		"trID.Root", hex.EncodeToString(trID.Root[:]),
	)
	if root == trID.Root {
		return nil
	}
	nodes := trienode.NewWithNodeSet(nodeSet)
	layerID := root
	if len(prefix) > 0 {
		// Just guarantees uniqueness
		hasher := sha3.NewLegacyKeccak256()
		hasher.Write(db.layerID[:])
		hasher.Write(root[:])
		layerID = common.BytesToHash(hasher.Sum(nil))
	}
	if err := db.triedb.Update(layerID, db.layerID, 0, nodes, nil); err != nil {
		return err
	}
	if len(prefix) == 0 {
		db.root = root
		fmt.Println("root updated", hex.EncodeToString(db.root[:]))
	}
	db.layerID = layerID
	db.lastRoots[common.BytesToHash(prefix)] = root
	return nil
}

func (db *db) Close() error {
	if err := db.triedb.Commit(db.layerID, false); err != nil {
		return err
	}
	if db.layerID != db.root {
		nodes := trienode.NewMergedNodeSet()
		if err := db.triedb.Update(db.root, db.layerID, 0, nodes, nil); err != nil {
			return err
		}
		if err := db.triedb.Commit(db.root, false); err != nil {
			return err
		}
	}
	return db.db.Put(rootKey, db.root[:])
}

func (db *db) GetChangeProof(ctx context.Context, startRootID, endRootID ids.ID, start, end maybe.Maybe[[]byte], maxLength int) (*ChangeProof, error) {
	startStateRoot := common.BytesToHash(startRootID[:])
	startTrie, err := trie.New(trie.StateTrieID(startStateRoot), db.triedb)
	if err != nil {
		return nil, merkledb.ErrInsufficientHistory
	}
	endStateRoot := common.BytesToHash(endRootID[:])
	endTrie, err := trie.New(trie.StateTrieID(endStateRoot), db.triedb)
	if err != nil {
		return nil, merkledb.ErrNoEndRoot
	}
	var additionalProof proof
	if len(start.Value()) == 64 {
		// This is a storage trie
		accHash := start.Value()[:32]
		startAccBytes, err := startTrie.Get(accHash)
		if err != nil {
			return nil, err
		}
		startAcc := new(types.StateAccount)
		if err := rlp.DecodeBytes(startAccBytes, startAcc); err != nil {
			return nil, err
		}

		endAccBytes, err := endTrie.Get(accHash)
		if err != nil {
			return nil, err
		}
		endAcc := new(types.StateAccount)
		if err := rlp.DecodeBytes(endAccBytes, endAcc); err != nil {
			return nil, err
		}

		// TODO: to prove the account root, we include this for now.
		// The client can find a better way to track this.
		if err := endTrie.Prove(accHash, (*proof)(&additionalProof)); err != nil {
			return nil, err
		}

		startTrie, err = trie.New(trie.StorageTrieID(startStateRoot, common.BytesToHash(accHash), startAcc.Root), db.triedb)
		if err != nil {
			return nil, merkledb.ErrInsufficientHistory
		}
		endTrie, err = trie.New(trie.StorageTrieID(endStateRoot, common.BytesToHash(accHash), endAcc.Root), db.triedb)
		if err != nil {
			return nil, merkledb.ErrNoEndRoot
		}

		if end.HasValue() {
			if len(end.Value()) != 64 {
				return nil, fmt.Errorf("invalid end key length: %d", len(end.Value()))
			}
			if !bytes.Equal(end.Value()[:32], accHash) {
				return nil, fmt.Errorf("end key does not match account hash: %x != %x", end.Value()[:32], accHash)
			}
			end = maybe.Some(end.Value()[32:])
		}
		start = maybe.Some(start.Value()[32:])
	}

	response, err := db.getChangeProof(ctx, startTrie, endTrie, startRootID, endRootID, start, end, maxLength)
	if err != nil {
		return nil, err
	}
	response.StartProof = append(response.StartProof, additionalProof...)
	return response, nil
}

func (db *db) getChangeProof(
	ctx context.Context, startTrie, endTrie *trie.Trie,
	startRootID, endRootID ids.ID, start, end maybe.Maybe[[]byte], maxLength int) (*ChangeProof, error) {
	startIt, err := startTrie.NodeIterator(start.Value())
	if err != nil {
		return nil, fmt.Errorf("failed to create start iterator: %w", err)
	}
	endIt, err := endTrie.NodeIterator(start.Value())
	if err != nil {
		return nil, fmt.Errorf("failed to create end iterator: %w", err)
	}

	startToEnd, _ := trie.NewDifferenceIterator(startIt, endIt)

	startIt, err = startTrie.NodeIterator(start.Value())
	if err != nil {
		return nil, fmt.Errorf("failed to create start iterator: %w", err)
	}
	endIt, err = endTrie.NodeIterator(start.Value())
	if err != nil {
		return nil, fmt.Errorf("failed to create end iterator: %w", err)
	}
	endToStart, _ := trie.NewDifferenceIterator(endIt, startIt)

	unionIt, _ := trie.NewUnionIterator([]trie.NodeIterator{startToEnd, endToStart})
	it := trie.NewIterator(unionIt)

	response := &ChangeProof{}
	for it.Next() {
		if len(response.KeyChanges) >= maxLength {
			break
		}
		if end.HasValue() && bytes.Compare(it.Key, end.Value()) > 0 {
			break
		}
		current, err := endTrie.Get(it.Key)
		if err != nil {
			current = nil
			if _, ok := err.(*trie.MissingNodeError); !ok {
				return nil, err
			}
		}
		currentVal := maybe.Nothing[[]byte]()
		if len(current) > 0 {
			currentVal = maybe.Some(current)
		}
		response.KeyChanges = append(response.KeyChanges, merkledb.KeyChange{
			Key:   bytes.Clone(it.Key),
			Value: currentVal,
		})
		//fmt.Println("--> CHANGE kv", hex.EncodeToString(it.Key), hex.EncodeToString(current))
	}

	// for i, it := range response.KeyChanges {
	// 	fmt.Println("--> CHANGE kv", i, hex.EncodeToString(it.Key), hex.EncodeToString(it.Value.Value()))
	// }
	startKey := start.Value()
	if len(startKey) == 0 && len(response.KeyChanges) > 0 {
		startKey = bytes.Repeat([]byte{0}, 32) // XXX
	}

	///////
	// Sanity check
	//////
	endKey := end.Value()
	if len(response.KeyChanges) > 0 {
		endKey = response.KeyChanges[len(response.KeyChanges)-1].Key
	}
	keyVals := make(map[string][]byte)
	if err := getKVsFromTrie(startTrie, startKey, endKey, keyVals); err != nil {
		panic("failed to get key-values")
	}
	for _, kv := range response.KeyChanges {
		val := kv.Value.Value()
		if len(val) == 0 {
			delete(keyVals, string(kv.Key))
			continue
		}
		keyVals[string(kv.Key)] = val
	}

	// Now get them from the end trie
	keyValsEnd := make(map[string][]byte)
	if err := getKVsFromTrie(endTrie, startKey, endKey, keyValsEnd); err != nil {
		panic("failed to get key-values")
	}
	// Make sure the key-values are the same
	if len(keyVals) != len(keyValsEnd) {
		panic("key-values don't match")
	}
	for key, val := range keyVals {
		if !bytes.Equal(val, keyValsEnd[key]) {
			panic("key-values don't match")
		}
	}
	/////

	tr := endTrie
	if err := tr.Prove(startKey, (*proof)(&response.StartProof)); err != nil {
		return nil, err
	}
	endProofKey := []byte{}
	if len(response.KeyChanges) > 0 {
		// If there is a non-zero number of keys, set [end] for the range proof to the last key.
		endProofKey = response.KeyChanges[len(response.KeyChanges)-1].Key
	} else if end.HasValue() { // is empty
		// If there are no keys, and [end] is set, set [end] for the range proof to [end].
		endProofKey = end.Value()
	}
	if err := tr.Prove(endProofKey, (*proof)(&response.EndProof)); err != nil {
		return nil, err
	}
	fmt.Println("CHANGE proof generated",
		"start", hex.EncodeToString(startKey),
		"end", hex.EncodeToString(end.Value()),
		"endProofKey", hex.EncodeToString(endProofKey),
		"startProof", len(response.StartProof),
		"endProof", len(response.EndProof),
		"keyValues", len(response.KeyChanges),
	)
	return response, nil
}

func (db *db) VerifyChangeProof(ctx context.Context, proof *ChangeProof, start, end maybe.Maybe[[]byte], expectedEndRootID ids.ID) error {
	var prefix []byte
	verifyRootID := expectedEndRootID
	if len(start.Value()) == 64 {
		prefix = start.Value()[:32]
		start = maybe.Some(start.Value()[32:])
		if end.HasValue() {
			end = maybe.Some(end.Value()[32:])
		}

		// If the proof is for a storage trie, the expected root ID should be
		// recovered from the "additionalProof", included in StartProof.
		proofDB := rawdb.NewMemoryDatabase()
		for _, node := range proof.StartProof {
			if err := proofDB.Put(node.Key.Bytes(), node.ValueOrHash.Value()); err != nil {
				return err
			}
		}
		val, err := trie.VerifyProof(
			common.BytesToHash(expectedEndRootID[:]),
			prefix,
			proofDB,
		)
		if err != nil {
			return fmt.Errorf("failed to verify proof: %w", err)
		}
		account := new(types.StateAccount)
		if err := rlp.DecodeBytes(val, account); err != nil {
			return fmt.Errorf("failed to decode account: %w", err)
		}

		verifyRootID = ids.ID(account.Root)
	}

	if err := db.verifyChangeProof(ctx, prefix, proof, start, end, verifyRootID); err != nil {
		return fmt.Errorf("failed to verify change proof: %w", err)
	}
	// HACK: tacking on start
	if len(proof.KeyChanges) > 0 {
		proof.KeyChanges = append(
			proof.KeyChanges,
			merkledb.KeyChange{Key: []byte("PREFIX"), Value: maybe.Some(prefix)},
		)
	}
	///
	return nil
}

func (db *db) verifyChangeProof(
	ctx context.Context, prefix []byte,
	proof *ChangeProof, start, end maybe.Maybe[[]byte], expectedEndRootID ids.ID) error {
	fmt.Println(
		"change proof verification",
		"start", hex.EncodeToString(start.Value()),
		"end", hex.EncodeToString(end.Value()),
		"expectedRootID", expectedEndRootID,
		"kvs", len(proof.KeyChanges),
	)
	switch {
	case start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) > 0:
		return ErrStartAfterEnd
	case proof.Empty():
		return ErrEmptyProof
	case end.HasValue() && len(proof.KeyChanges) == 0 && len(proof.EndProof) == 0:
		// We requested an end proof but didn't get one.
		return ErrNoEndProof
	case start.HasValue() && len(proof.StartProof) == 0 && len(proof.EndProof) == 0:
		// We requested a start proof but didn't get one.
		// Note that we also have to check that [proof.EndProof] is empty
		// to handle the case that the start proof is empty because all
		// its nodes are also in the end proof, and those nodes are omitted.
		return ErrNoStartProof
	}

	proofDB := rawdb.NewMemoryDatabase()
	for _, node := range proof.StartProof {
		if err := proofDB.Put(node.Key.Bytes(), node.ValueOrHash.Value()); err != nil {
			return err
		}
	}
	for _, node := range proof.EndProof {
		if err := proofDB.Put(node.Key.Bytes(), node.ValueOrHash.Value()); err != nil {
			return err
		}
	}

	startKey := start.Value()
	if len(startKey) == 0 {
		startKey = bytes.Repeat([]byte{0}, 32) // XXX
	}

	kvs := make(map[string][]byte)
	endOfProofRange := end.Value()
	if len(proof.KeyChanges) > 0 {
		endOfProofRange = proof.KeyChanges[len(proof.KeyChanges)-1].Key
	}
	if err := db.getKVs(prefix, startKey, endOfProofRange, kvs); err != nil {
		return fmt.Errorf("failed to get key-values: %w", err)
	}
	fmt.Println("kvs", len(kvs))
	for _, kv := range proof.KeyChanges {
		if len(kv.Key) != 32 {
			return fmt.Errorf("invalid key length: %d", len(kv.Key))
		}
		val := kv.Value.Value()

		kvs[string(kv.Key)] = val
	}

	keys := make([][]byte, 0, len(kvs))
	for key := range kvs {
		keys = append(keys, []byte(key))
	}
	slices.SortFunc(keys, bytes.Compare)
	vals := make([][]byte, len(keys))
	for i, key := range keys {
		vals[i] = kvs[string(key)]
	}

	root := common.BytesToHash(expectedEndRootID[:])
	if len(kvs) == 0 && end.HasValue() { // XXX: should have proper isEmpty
		if err := trie.VerifyRangeProofEmpty(root, startKey, end.Value(), proofDB); err != nil {
			fmt.Println("proof verification failed empty", err)
			return err
		}
	} else {
		if len(proof.KeyChanges) == 0 && len(kvs) > 0 {
			// No changes here, but we have some KVs in the tree.
			// The proof was created with end.Value() as the end of the range.
			if end.HasValue() {
				lastKey := keys[len(keys)-1]
				// This means end.Value() is not in the trie.
				// So, we can consider it as a deletion.
				if bytes.Compare(lastKey, end.Value()) < 0 {
					keys = append(keys, end.Value())
					vals = append(vals, nil)
				}
			}
		}
		if _, err := trie.VerifyRangeProofAllowDeletions(root, startKey, keys, vals, proofDB); err != nil {
			fmt.Println("proof verification failed", err, "kvs", len(keys))
			return err
		}
	}

	fmt.Println(
		"change proof verification SUCCESS",
		"start", hex.EncodeToString(start.Value()),
		"end", hex.EncodeToString(end.Value()),
		"expectedRootID", expectedEndRootID,
		"kvs", len(proof.KeyChanges),
	)
	return nil
}

// assumes updateLock is held
func (db *db) getTrieID(prefix []byte) *trie.ID {
	owner := common.BytesToHash(prefix)
	root := db.lastRoots[owner]
	if root == (common.Hash{}) {
		root = types.EmptyRootHash
	}
	return trie.StorageTrieID(db.layerID, owner, root)
}

func (db *db) getKVs(prefix []byte, startKey, endKey []byte, kvs map[string][]byte) error {
	db.updateLock.RLock()
	defer db.updateLock.RUnlock()

	tr, err := trie.New(db.getTrieID(prefix), db.triedb)
	if err != nil {
		return err
	}
	return getKVsFromTrie(tr, startKey, endKey, kvs)
}

func getKVsFromTrie(tr *trie.Trie, startKey, endKey []byte, kvs map[string][]byte) error {
	nodeIt, err := tr.NodeIterator(startKey)
	if err != nil {
		return err
	}
	it := trie.NewIterator(nodeIt)
	for it.Next() {
		if len(endKey) > 0 && bytes.Compare(it.Key, endKey) > 0 {
			break
		}
		kvs[string(it.Key)] = bytes.Clone(it.Value)
	}
	return nil
}

func (db *db) CommitChangeProof(ctx context.Context, proof *ChangeProof) error {
	// UNHACK: remove prefix
	last := proof.KeyChanges[len(proof.KeyChanges)-1]
	if !bytes.Equal(last.Key, []byte("PREFIX")) {
		panic("invalid prefix")
	}
	prefix := last.Value.Value()
	proof.KeyChanges = proof.KeyChanges[:len(proof.KeyChanges)-1]
	///

	keyValues := make([]KeyValue, 0, len(proof.KeyChanges))
	for _, change := range proof.KeyChanges {
		keyValues = append(keyValues, KeyValue{
			Key:   change.Key,
			Value: change.Value.Value(),
		})
	}

	return db.updateKVs(prefix, keyValues)
}

func (db *db) Put(key, value []byte) error {
	return db.updateKVs(nil, []KeyValue{{Key: key, Value: value}})
}

func (db *db) NewBatch() *trieBatch {
	return &trieBatch{db: db}
}

type trieBatch struct {
	db  *db
	kvs []KeyValue
}

func (t *trieBatch) Put(key, value []byte) error {
	t.kvs = append(t.kvs, KeyValue{Key: key, Value: value})
	return nil
}

func (t *trieBatch) Write() error {
	return t.db.updateKVs(nil, t.kvs)
}

func (db *db) IterateOneKey(key []byte) ([]byte, bool) {
	db.updateLock.RLock()
	defer db.updateLock.RUnlock()

	tr, err := trie.New(trie.StateTrieID(db.root), db.triedb)
	if err != nil {
		panic("failed to create trie")
	}
	nodeIt, err := tr.NodeIterator(key)
	if err != nil {
		panic("failed to create iterator")
	}
	it := trie.NewIterator(nodeIt)
	if !it.Next() {
		return nil, false
	}
	return it.Key, true
}
