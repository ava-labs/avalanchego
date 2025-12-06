// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package firewood

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"slices"
	"testing"

	firewood "github.com/ava-labs/firewood-go/ffi"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/stretchr/testify/require"
)

const (
	keySize      = 4
	valSize      = 4
	maxBatchSize = 5
	maxNumKeys   = 10_000
)

const (
	dbGet byte = iota
	dbUpdate
	dbBatch
	checkDBHash
	createProposalOnProposal
	createProposalOnDB
	proposalGet
	commitProposal
	maxStep
)

var stepMap = map[byte]string{
	dbGet:                    "dbGet",
	dbUpdate:                 "dbUpdate",
	dbBatch:                  "dbBatch",
	checkDBHash:              "checkDBHash",
	createProposalOnProposal: "createProposalOnProposal",
	createProposalOnDB:       "createProposalOnDB",
	proposalGet:              "proposalGet",
	commitProposal:           "commitProposal",
}

func newTestFirewoodDatabase(t *testing.T) *firewood.Database {
	t.Helper()

	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := newFirewoodDatabase(dbFile)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close(context.Background())) //nolint:usetesting // t.Context() will already be cancelled
	})
	return db
}

func newFirewoodDatabase(dbFile string) (*firewood.Database, error) {
	conf := firewood.DefaultConfig()
	f, err := firewood.New(dbFile, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to create new database at filepath %q: %w", dbFile, err)
	}
	return f, nil
}

type tree struct {
	require *require.Assertions
	rand    *rand.Rand

	id       int
	nextID   int
	merkleDB merkledb.MerkleDB
	fwdDB    *firewood.Database

	children []*proposal

	proposals map[int]*proposal
	keys      [][]byte
}

type proposal struct {
	parentID   int
	id         int
	merkleView merkledb.View
	fwdView    *firewood.Proposal

	children []*proposal
}

func newTestTree(t *testing.T, rand *rand.Rand) *tree {
	r := require.New(t)

	memdb := memdb.New()
	merkleDB, err := merkledb.New(context.Background(), memdb, merkledb.NewConfig())
	r.NoError(err)

	tr := &tree{
		merkleDB:  merkleDB,
		fwdDB:     newTestFirewoodDatabase(t),
		proposals: make(map[int]*proposal),
		children:  make([]*proposal, 0),
		require:   r,
		rand:      rand,
		keys:      createRandomByteSlices(min(1, rand.Intn(maxNumKeys)), keySize, rand),
	}
	return tr
}

func (tr *tree) selectRandomProposal() *proposal {
	if len(tr.proposals) == 0 {
		return nil
	}

	proposalIDs := make([]int, 0, len(tr.proposals))
	for proposalID := range tr.proposals {
		proposalIDs = append(proposalIDs, proposalID)
	}
	slices.Sort(proposalIDs)
	selectedProposalID := tr.rand.Intn(len(proposalIDs))
	return tr.proposals[selectedProposalID]
}

func (tr *tree) dropAllProposals() {
	// Drop all firewood proposals out of order (allowed by firewood).
	for _, p := range tr.proposals {
		tr.require.NoError(p.fwdView.Drop())
	}
	// Free pointers at the root to allow garbage collection.
	// MerkleDB does not require explicitly dropping merkle views, so clearing the
	// pointers at the root is sufficient.
	tr.children = nil
	tr.proposals = make(map[int]*proposal)
}

func (tr *tree) dbUpdate() {
	key := tr.keys[tr.rand.Intn(len(tr.keys))]
	val := createRandomSlice(valSize, tr.rand)

	// Insert the key-value pair into both databases.
	tr.require.NoError(tr.merkleDB.Put(key, val))
	_, err := tr.fwdDB.Update([][]byte{key}, [][]byte{val})
	tr.require.NoError(err)

	tr.dropAllProposals()
}

func (tr *tree) createRandomBatch(numKeys int) ([][]byte, [][]byte) {
	keys := tr.selectRandomKeys(numKeys)
	vals := createRandomByteSlices(len(keys), valSize, tr.rand)
	return keys, vals
}

func (tr *tree) selectRandomKeys(numKeys int) [][]byte {
	numKeys = min(numKeys, len(tr.keys))
	selectedKeys := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		selectedKeys[i] = tr.keys[tr.rand.Intn(len(tr.keys))]
	}
	return selectedKeys
}

func (tr *tree) dbGet() {
	keys := tr.selectRandomKeys(maxBatchSize)
	for _, key := range keys {
		fwdVal, fwdErr := tr.fwdDB.Get(key)
		merkleVal, merkleErr := tr.merkleDB.GetValue(context.Background(), key)
		tr.testGetResult(merkleVal, merkleErr, fwdVal, fwdErr)
	}
}

func (tr *tree) testGetResult(merkleVal []byte, merkleErr error, fwdVal []byte, fwdErr error) {
	if merkleErr == database.ErrNotFound {
		tr.require.Nil(merkleVal)
		tr.require.NoError(fwdErr)
		tr.require.Nil(fwdVal)
		return
	}
	tr.require.NoError(merkleErr)
	tr.require.NoError(fwdErr)
	tr.require.Equal(fwdVal, merkleVal)
}

func (tr *tree) dbBatch() {
	batchSize := tr.rand.Intn(maxBatchSize) + 1 // XXX: ensure at least one KV pair to avoid firewood empty revision errors
	keys, vals := tr.createRandomBatch(batchSize)

	batch := tr.merkleDB.NewBatch()
	for i := range len(keys) {
		tr.require.NoError(batch.Put(keys[i], vals[i]))
	}
	tr.require.NoError(batch.Write())

	_, err := tr.fwdDB.Update(keys, vals)
	tr.require.NoError(err)

	tr.dropAllProposals()
}

func (tr *tree) commitRandomProposal() {
	childIndex := tr.rand.Intn(len(tr.children)) // assumes non-zero number of children

	commitProposal := tr.children[childIndex]
	tr.require.NoError(commitProposal.fwdView.Commit())
	tr.require.NoError(commitProposal.merkleView.CommitToDB(context.Background()))
	delete(tr.proposals, commitProposal.id)

	remainingChildren := tr.children
	for i := 0; i < len(remainingChildren); i++ {
		dropChild := remainingChildren[i]
		if dropChild.id == commitProposal.id {
			continue
		}
		tr.require.NoError(dropChild.fwdView.Drop())
		delete(tr.proposals, dropChild.id)

		remainingChildren = append(remainingChildren, dropChild.children...)
	}

	tr.children = commitProposal.children
}

func (tr *tree) checkDBHash() {
	// Get the root hash from both databases.
	merkleRoot, err := tr.merkleDB.GetMerkleRoot(context.Background())
	tr.require.NoError(err)

	fwdRoot, err := tr.fwdDB.Root()
	tr.require.NoError(err)

	// Compare the root hashes.
	tr.require.Equal(merkleRoot, ids.ID(fwdRoot))
}

func (tr *tree) createProposalOnProposal() {
	pr := tr.selectRandomProposal()

	if pr == nil {
		return
	}

	batchSize := tr.rand.Intn(maxBatchSize) + 1 // ensure at least one key-value pair
	keys, vals := tr.createRandomBatch(batchSize)

	fwdPr := pr.fwdView
	merkleView := pr.merkleView

	fwdChildPr, err := fwdPr.Propose(keys, vals)
	tr.require.NoError(err)

	merkleViewChange := merkledb.ViewChanges{}
	for i := range keys {
		merkleViewChange.BatchOps = append(merkleViewChange.BatchOps, database.BatchOp{
			Key:   keys[i],
			Value: vals[i],
		})
	}
	merkleChildView, err := merkleView.NewView(context.Background(), merkleViewChange)
	tr.require.NoError(err)

	fwdRoot, err := fwdChildPr.Root()
	tr.require.NoError(err)
	merkleRoot, err := merkleChildView.GetMerkleRoot(context.Background())
	tr.require.NoError(err)
	tr.require.Equal(merkleRoot, ids.ID(fwdRoot))

	tr.nextID++
	newProposal := &proposal{
		parentID:   pr.id,
		id:         tr.nextID,
		merkleView: merkleChildView,
		fwdView:    fwdChildPr,
	}
	pr.children = append(pr.children, newProposal)
	tr.proposals[newProposal.id] = newProposal
}

func (tr *tree) createProposalOnDB() {
	batchSize := tr.rand.Intn(maxBatchSize) + 1 // ensure at least one key-value pair
	keys, vals := tr.createRandomBatch(batchSize)
	fwdPr, err := tr.fwdDB.Propose(keys, vals)
	tr.require.NoError(err)

	merkleViewChange := merkledb.ViewChanges{}
	for i := range keys {
		merkleViewChange.BatchOps = append(merkleViewChange.BatchOps, database.BatchOp{
			Key:   keys[i],
			Value: vals[i],
		})
	}
	merkleChildView, err := tr.merkleDB.NewView(context.Background(), merkleViewChange)
	tr.require.NoError(err)

	fwdRoot, err := fwdPr.Root()
	tr.require.NoError(err)
	merkleRoot, err := merkleChildView.GetMerkleRoot(context.Background())
	tr.require.NoError(err)
	tr.require.Equal(merkleRoot, ids.ID(fwdRoot))

	tr.nextID++
	newProposal := &proposal{
		parentID:   tr.id,
		id:         tr.nextID,
		merkleView: merkleChildView,
		fwdView:    fwdPr,
	}
	tr.children = append(tr.children, newProposal)
	tr.proposals[newProposal.id] = newProposal
}

func (tr *tree) proposalGet() {
	pr := tr.selectRandomProposal()
	if pr == nil {
		return
	}

	keys := tr.selectRandomKeys(maxBatchSize)
	for _, key := range keys {
		fwdVal, fwdErr := pr.fwdView.Get(key)
		merkleVal, merkleErr := pr.merkleView.GetValue(context.Background(), key)

		tr.testGetResult(merkleVal, merkleErr, fwdVal, fwdErr)
	}
}

func (tr *tree) commitProposal() {
	if len(tr.children) == 0 {
		return // no proposals to commit
	}
	tr.commitRandomProposal()
}

func fuzzTree(t *testing.T, randSource int64, byteSteps []byte) {
	rand := rand.New(rand.NewSource(randSource))

	if len(byteSteps) > 100 {
		byteSteps = byteSteps[:100] // limit the number of steps to 100
	}

	tr := newTestTree(t, rand)

	// TODO: replace randomly generated values with bytes from the fuzzer
	for _, step := range byteSteps {
		step = step % maxStep
		// Make this two lines so debugger displays the stepStr value
		stepStr := stepMap[step]
		t.Log(stepStr)
		switch step {
		case dbGet:
			tr.dbGet()
		case dbUpdate:
			tr.dbUpdate()
		case dbBatch:
			tr.dbBatch()
		case checkDBHash:
			tr.checkDBHash()
		case createProposalOnProposal:
			tr.createProposalOnProposal()
		case createProposalOnDB:
			tr.createProposalOnDB()
		case proposalGet:
			tr.proposalGet()
		case commitProposal:
			tr.commitProposal()
		}
	}
}

func FuzzTree(f *testing.F) {
	// Add interesting sequences to the fuzzer with a few different random seeds.
	for i := range 5 {
		f.Add(int64(i), []byte{
			createProposalOnDB,
			createProposalOnDB,
			createProposalOnProposal,
			createProposalOnProposal,
			createProposalOnDB,
			dbGet,
			proposalGet,
			createProposalOnDB,
			createProposalOnProposal,
			commitProposal,
			checkDBHash,
			createProposalOnDB,
			createProposalOnProposal,
			commitProposal,
		})
		f.Add(int64(i), []byte{
			dbUpdate,
			dbGet,
			dbBatch,
			createProposalOnDB,
			checkDBHash,
			createProposalOnProposal,
			commitProposal,
			checkDBHash,
			commitProposal,
		})
		f.Add(int64(i), []byte{
			dbBatch,
			dbBatch,
			dbBatch,
			dbGet,
			createProposalOnDB,
			createProposalOnProposal,
			commitProposal,
			commitProposal,
			checkDBHash,
		})
	}
	f.Fuzz(fuzzTree)
}

func createRandomSlice(maxSize int, rand *rand.Rand) []byte {
	sliceSize := rand.Intn(maxSize) + 1 // always create non-empty slices
	slice := make([]byte, sliceSize)
	_, err := rand.Read(slice)
	if err != nil {
		panic(err)
	}
	return slice
}

func createRandomByteSlices(numSlices int, maxSliceSize int, rand *rand.Rand) [][]byte {
	slices := make([][]byte, numSlices)
	for i := 0; i < numSlices; i++ {
		slices[i] = createRandomSlice(maxSliceSize, rand)
	}
	return slices
}
