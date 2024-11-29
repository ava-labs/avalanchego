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

var rootKey = []byte{'r', 'o', 'o', 't'}

type db struct {
	db         ethdb.Database
	triedb     *triedb.Database
	root       common.Hash
	accountID  ids.ID
	updateLock sync.RWMutex
}

func New(ctx context.Context, disk database.Database, config merkledb.Config) (*db, error) {
	return NewWithAccount(ctx, disk, ids.Empty)
}

func NewWithAccount(ctx context.Context, disk database.Database, accountID ids.ID) (*db, error) {
	ethdb := rawdb.NewDatabase(evm.Database{Database: disk})
	root := types.EmptyRootHash
	diskRoot, err := ethdb.Get(mkRootKey(accountID))
	if err == nil {
		copy(root[:], diskRoot)
	} else if err != database.ErrNotFound {
		return nil, err
	}

	triedb := triedb.NewDatabase(ethdb, &triedb.Config{PathDB: pathdb.Defaults})
	return &db{
		db:     ethdb,
		triedb: triedb,
		root:   root,
	}, nil
}

func mkRootKey(accountID ids.ID) []byte {
	if accountID == (ids.ID{}) {
		return rootKey
	}
	return append(accountID[:], rootKey...)
}

func (db *db) Clear() error {
	for {
		batchSize := 1000
		batch := make(map[string][]byte, batchSize)
		if err := db.getKVs(nil, nil, batch, batchSize); err != nil {
			return fmt.Errorf("failed to get key-values: %w", err)
		}
		if len(batch) == 0 {
			// No more keys to delete, we're done
			break
		}
		updates := make([]KeyValue, 0, len(batch))
		for key := range batch {
			// Delete each key
			updates = append(updates, KeyValue{Key: []byte(key), Value: nil})
		}
		if err := db.updateKVs(updates); err != nil {
			return fmt.Errorf("failed to clear keys: %w", err)
		}
	}

	db.root = types.EmptyRootHash
	return db.db.Delete(mkRootKey(db.accountID))
}

func (db *db) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	db.updateLock.RLock()
	defer db.updateLock.RUnlock()

	if db.root == types.EmptyRootHash {
		return ids.ID{}, nil
	}
	return ids.ID(db.root), nil
}

func (db *db) GetRangeProofAtRoot(ctx context.Context, rootID ids.ID, start, end maybe.Maybe[[]byte], maxLength int, trieIDs ...ids.ID) (*RangeProof, error) {
	trieID, err := db.getTrieID(rootID, trieIDs...)
	if err != nil {
		return nil, err
	}
	response := &RangeProof{}
	tr, err := trie.New(trieID, db.triedb)
	if err != nil {
		return nil, err
	}
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
	return db.updateKVs(proof.KeyValues)
}

func (db *db) NextKey(key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key length is not 32")
	}
	keyCopy := bytes.Clone(key)
	IncrOne(keyCopy)
	return keyCopy, nil
}

func (db *db) updateKVs(kvs []merkledb.KeyValue) error {
	db.updateLock.Lock()
	defer db.updateLock.Unlock()

	tr, err := trie.New(db.openableTrieID(), db.triedb)
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
	root, nodeSet, err := tr.Commit(false)
	if err != nil {
		return err
	}
	fmt.Fprintln(
		os.Stderr,
		"committing", len(kvs),
		"parent", hex.EncodeToString(db.root[:]),
		"root", hex.EncodeToString(root[:]),
	)
	if root == db.root {
		return nil
	}
	nodes := trienode.NewWithNodeSet(nodeSet)
	if err := db.triedb.Update(root, db.root, 0, nodes, nil); err != nil {
		return err
	}
	db.root = root
	return nil
}

func (db *db) Close() error {
	if err := db.triedb.Commit(db.root, false); err != nil {
		return err
	}
	return db.db.Put(mkRootKey(db.accountID), db.root[:])
}

func (db *db) getTrieID(stateRootID ids.ID, trieIDs ...ids.ID) (*trie.ID, error) {
	stateRoot := common.BytesToHash(stateRootID[:])
	if len(trieIDs) == 0 {
		return trie.StateTrieID(stateRoot), nil
	}
	owner := common.BytesToHash(trieIDs[0][:])
	if owner == (common.Hash{}) {
		return trie.StateTrieID(stateRoot), nil
	}

	stateTrie, err := trie.NewStateTrie(trie.StateTrieID(stateRoot), db.triedb)
	if err != nil {
		return nil, err
	}
	account, err := stateTrie.GetAccountByHash(owner)
	if err != nil {
		return nil, err
	}
	return trie.StorageTrieID(stateRoot, owner, account.Root), nil
}

func (db *db) GetChangeProof(ctx context.Context, startRootID, endRootID ids.ID, start, end maybe.Maybe[[]byte], maxLength int, trieIDs ...ids.ID) (*ChangeProof, error) {
	startTrieID, err := db.getTrieID(startRootID, trieIDs...)
	if err != nil {
		return nil, merkledb.ErrInsufficientHistory
	}
	startTrie, err := trie.New(startTrieID, db.triedb)
	if err != nil {
		return nil, merkledb.ErrInsufficientHistory
	}
	endTrieID, err := db.getTrieID(endRootID, trieIDs...)
	if err != nil {
		return nil, merkledb.ErrNoEndRoot
	}
	endTrie, err := trie.New(endTrieID, db.triedb)
	if err != nil {
		return nil, merkledb.ErrNoEndRoot
	}

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
		fmt.Println("--> CHANGE kv", hex.EncodeToString(it.Key), hex.EncodeToString(current))
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
	if err := getKVsFromTrie(startTrie, startKey, endKey, keyVals, 0); err != nil {
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
	if err := getKVsFromTrie(endTrie, startKey, endKey, keyValsEnd, 0); err != nil {
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
	if err := db.getKVs(startKey, endOfProofRange, kvs, 0); err != nil {
		return fmt.Errorf("failed to get key-values: %w", err)
	}
	fmt.Println("kvs", len(kvs))
	for _, kv := range proof.KeyChanges {
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

// openableTrieID returns the trie ID that can be opened by the trie database,
// to read current values or update them.
// assumes updateLock is held for reading
func (db *db) openableTrieID() *trie.ID {
	if db.accountID == (ids.ID{}) {
		return trie.StateTrieID(db.root)
	}
	// XXX: passing db.root here but maybe should retain the original state root?
	// Is this even meaningful as there would be no "state-root" corresponding
	// to partially synced storage tries.
	return trie.StorageTrieID(db.root, common.Hash(db.accountID), db.root)
}

// getKVs returns the key-values in the trie in the range [startKey, endKey].
// If limit is non-zero, at most limit key-values are returned.
func (db *db) getKVs(startKey, endKey []byte, kvs map[string][]byte, limit int) error {
	db.updateLock.Lock()
	defer db.updateLock.Unlock()

	tr, err := trie.New(db.openableTrieID(), db.triedb)
	if err != nil {
		return err
	}
	return getKVsFromTrie(tr, startKey, endKey, kvs, limit)
}

func getKVsFromTrie(tr *trie.Trie, startKey, endKey []byte, kvs map[string][]byte, limit int) error {
	nodeIt, err := tr.NodeIterator(startKey)
	if err != nil {
		return err
	}
	it := trie.NewIterator(nodeIt)
	for it.Next() {
		if len(endKey) > 0 && bytes.Compare(it.Key, endKey) > 0 {
			break
		}
		if limit > 0 && len(kvs) >= limit {
			break
		}
		kvs[string(it.Key)] = bytes.Clone(it.Value)
	}
	return nil
}

func (db *db) CommitChangeProof(ctx context.Context, proof *ChangeProof) error {
	keyValues := make([]KeyValue, 0, len(proof.KeyChanges))
	for _, change := range proof.KeyChanges {
		keyValues = append(keyValues, KeyValue{
			Key:   change.Key,
			Value: change.Value.Value(),
		})
	}

	return db.updateKVs(keyValues)
}

func (db *db) Put(key, value []byte) error {
	return db.updateKVs([]KeyValue{{Key: key, Value: value}})
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
	return t.db.updateKVs(t.kvs)
}

func (db *db) IterateOneKey(key []byte) ([]byte, bool) {
	db.updateLock.RLock()
	defer db.updateLock.RUnlock()

	tr, err := trie.New(db.openableTrieID(), db.triedb)
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
