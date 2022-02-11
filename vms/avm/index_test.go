// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/index"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var indexEnabledAvmConfig = Config{
	IndexTransactions: true,
}

func TestIndexTransaction_Ordered(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	ctx := NewContext(t)
	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	vm := setupTestVM(t, ctx, baseDBManager, genesisBytes, issuer, indexEnabledAvmConfig)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	key := keys[0]
	addr := key.PublicKey().Address()

	var uniqueTxs []*UniqueTx
	txAssetID := avax.Asset{ID: avaxID}

	ctx.Lock.Lock()
	for i := 0; i < 5; i++ {
		// create utxoID and assetIDs
		utxoID := avax.UTXOID{
			TxID: ids.GenerateTestID(),
		}

		// build the transaction
		tx := buildTX(utxoID, txAssetID, addr)

		// sign the transaction
		if err := signTX(vm.codec, tx, key); err != nil {
			t.Fatal(err)
		}

		// Provide the platform UTXO
		utxo := buildPlatformUTXO(utxoID, txAssetID, addr)

		// save utxo to state
		inputID := utxo.InputID()
		if err := vm.state.PutUTXO(inputID, utxo); err != nil {
			t.Fatal("Error saving utxo", err)
		}

		// issue transaction
		if _, err := vm.IssueTx(tx.Bytes()); err != nil {
			t.Fatalf("should have issued the transaction correctly but errored: %s", err)
		}

		ctx.Lock.Unlock()

		msg := <-issuer
		if msg != common.PendingTxs {
			t.Fatalf("Wrong message")
		}

		ctx.Lock.Lock()

		// get pending transactions
		txs := vm.PendingTxs()
		if len(txs) != 1 {
			t.Fatalf("Should have returned %d tx(s)", 1)
		}

		parsedTx := txs[0]
		uniqueParsedTX := parsedTx.(*UniqueTx)
		uniqueTxs = append(uniqueTxs, uniqueParsedTX)

		var inputUTXOs []*avax.UTXO
		for _, utxoID := range uniqueParsedTX.InputUTXOs() {
			utxo, err := vm.getUTXO(utxoID)
			if err != nil {
				t.Fatal(err)
			}

			inputUTXOs = append(inputUTXOs, utxo)
		}

		// index the transaction
		err := vm.addressTxsIndexer.Accept(uniqueParsedTX.ID(), inputUTXOs, uniqueParsedTX.UTXOs())
		assert.NoError(t, err)
	}

	// ensure length is 5
	assert.Len(t, uniqueTxs, 5)
	// for each *UniqueTx check its indexed at right index
	for i, tx := range uniqueTxs {
		assertIndexedTX(t, vm.db, uint64(i), addr, txAssetID.ID, tx.ID())
	}

	assertLatestIdx(t, vm.db, addr, txAssetID.ID, 5)
}

func TestIndexTransaction_MultipleTransactions(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	ctx := NewContext(t)
	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()
	vm := setupTestVM(t, ctx, baseDBManager, genesisBytes, issuer, indexEnabledAvmConfig)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	addressTxMap := map[ids.ShortID]*UniqueTx{}
	txAssetID := avax.Asset{ID: avaxID}

	ctx.Lock.Lock()
	for _, key := range keys {
		addr := key.PublicKey().Address()
		// create utxoID and assetIDs
		utxoID := avax.UTXOID{
			TxID: ids.GenerateTestID(),
		}

		// build the transaction
		tx := buildTX(utxoID, txAssetID, addr)

		// sign the transaction
		if err := signTX(vm.codec, tx, key); err != nil {
			t.Fatal(err)
		}

		// Provide the platform UTXO
		utxo := buildPlatformUTXO(utxoID, txAssetID, addr)

		// save utxo to state
		inputID := utxo.InputID()
		if err := vm.state.PutUTXO(inputID, utxo); err != nil {
			t.Fatal("Error saving utxo", err)
		}

		// issue transaction
		if _, err := vm.IssueTx(tx.Bytes()); err != nil {
			t.Fatalf("should have issued the transaction correctly but errored: %s", err)
		}

		ctx.Lock.Unlock()

		msg := <-issuer
		if msg != common.PendingTxs {
			t.Fatalf("Wrong message")
		}

		ctx.Lock.Lock()

		// get pending transactions
		txs := vm.PendingTxs()
		if len(txs) != 1 {
			t.Fatalf("Should have returned %d tx(s)", 1)
		}

		parsedTx := txs[0]
		uniqueParsedTX := parsedTx.(*UniqueTx)
		addressTxMap[addr] = uniqueParsedTX

		var inputUTXOs []*avax.UTXO
		for _, utxoID := range uniqueParsedTX.InputUTXOs() {
			utxo, err := vm.getUTXO(utxoID)
			if err != nil {
				t.Fatal(err)
			}

			inputUTXOs = append(inputUTXOs, utxo)
		}

		// index the transaction
		err := vm.addressTxsIndexer.Accept(uniqueParsedTX.ID(), inputUTXOs, uniqueParsedTX.UTXOs())
		assert.NoError(t, err)
	}

	// ensure length is same as keys length
	assert.Len(t, addressTxMap, len(keys))

	// for each *UniqueTx check its indexed at right index for the right address
	for key, tx := range addressTxMap {
		assertIndexedTX(t, vm.db, uint64(0), key, txAssetID.ID, tx.ID())
		assertLatestIdx(t, vm.db, key, txAssetID.ID, 1)
	}
}

func TestIndexTransaction_MultipleAddresses(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	ctx := NewContext(t)
	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()
	vm := setupTestVM(t, ctx, baseDBManager, genesisBytes, issuer, indexEnabledAvmConfig)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	txAssetID := avax.Asset{ID: avaxID}
	addrs := make([]ids.ShortID, len(keys))
	for _, key := range keys {
		addrs = append(addrs, key.PublicKey().Address())
	}

	ctx.Lock.Lock()

	key := keys[0]
	addr := key.PublicKey().Address()
	// create utxoID and assetIDs
	utxoID := avax.UTXOID{
		TxID: ids.GenerateTestID(),
	}

	// build the transaction
	tx := buildTX(utxoID, txAssetID, addrs...)

	// sign the transaction
	if err := signTX(vm.codec, tx, key); err != nil {
		t.Fatal(err)
	}

	// Provide the platform UTXO
	utxo := buildPlatformUTXO(utxoID, txAssetID, addr)

	// save utxo to state
	inputID := utxo.InputID()
	if err := vm.state.PutUTXO(inputID, utxo); err != nil {
		t.Fatal("Error saving utxo", err)
	}

	var inputUTXOs []*avax.UTXO //nolint:prealloc
	for _, utxoID := range tx.InputUTXOs() {
		utxo, err := vm.getUTXO(utxoID)
		if err != nil {
			t.Fatal(err)
		}

		inputUTXOs = append(inputUTXOs, utxo)
	}

	// index the transaction
	err := vm.addressTxsIndexer.Accept(tx.ID(), inputUTXOs, tx.UTXOs())
	assert.NoError(t, err)
	assert.NoError(t, err)

	assertIndexedTX(t, vm.db, uint64(0), addr, txAssetID.ID, tx.ID())
	assertLatestIdx(t, vm.db, addr, txAssetID.ID, 1)
}

func TestIndexTransaction_UnorderedWrites(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	ctx := NewContext(t)
	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)
	avaxID := genesisTx.ID()
	vm := setupTestVM(t, ctx, baseDBManager, genesisBytes, issuer, indexEnabledAvmConfig)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	addressTxMap := map[ids.ShortID]*UniqueTx{}
	txAssetID := avax.Asset{ID: avaxID}

	ctx.Lock.Lock()
	for _, key := range keys {
		addr := key.PublicKey().Address()
		// create utxoID and assetIDs
		utxoID := avax.UTXOID{
			TxID: ids.GenerateTestID(),
		}

		// build the transaction
		tx := buildTX(utxoID, txAssetID, addr)

		// sign the transaction
		if err := signTX(vm.codec, tx, key); err != nil {
			t.Fatal(err)
		}

		// Provide the platform UTXO
		utxo := buildPlatformUTXO(utxoID, txAssetID, addr)

		// save utxo to state
		inputID := utxo.InputID()
		if err := vm.state.PutUTXO(inputID, utxo); err != nil {
			t.Fatal("Error saving utxo", err)
		}

		// issue transaction
		if _, err := vm.IssueTx(tx.Bytes()); err != nil {
			t.Fatalf("should have issued the transaction correctly but errored: %s", err)
		}

		ctx.Lock.Unlock()

		msg := <-issuer
		if msg != common.PendingTxs {
			t.Fatalf("Wrong message")
		}

		ctx.Lock.Lock()

		// get pending transactions
		txs := vm.PendingTxs()
		if len(txs) != 1 {
			t.Fatalf("Should have returned %d tx(s)", 1)
		}

		parsedTx := txs[0]
		uniqueParsedTX := parsedTx.(*UniqueTx)
		addressTxMap[addr] = uniqueParsedTX

		var inputUTXOs []*avax.UTXO
		for _, utxoID := range uniqueParsedTX.InputUTXOs() {
			utxo, err := vm.getUTXO(utxoID)
			if err != nil {
				t.Fatal(err)
			}

			inputUTXOs = append(inputUTXOs, utxo)
		}

		// index the transaction, NOT calling Accept(ids.ID) method
		err := vm.addressTxsIndexer.Accept(uniqueParsedTX.ID(), inputUTXOs, uniqueParsedTX.UTXOs())
		assert.NoError(t, err)
	}

	// ensure length is same as keys length
	assert.Len(t, addressTxMap, len(keys))

	// for each *UniqueTx check its indexed at right index for the right address
	for key, tx := range addressTxMap {
		assertIndexedTX(t, vm.db, uint64(0), key, txAssetID.ID, tx.ID())
		assertLatestIdx(t, vm.db, key, txAssetID.ID, 1)
	}
}

func TestIndexer_Read(t *testing.T) {
	// setup vm, db etc
	_, vm, _, _, _ := setup(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// generate test address and asset IDs
	assetID := ids.GenerateTestID()
	addr := ids.GenerateTestShortID()

	// setup some fake txs under the above generated address and asset IDs
	testTxCount := 25
	testTxs := setupTestTxsInDB(t, vm.db, addr, assetID, testTxCount)
	assert.Len(t, testTxs, 25)

	// read the pages, 5 items at a time
	var cursor uint64
	var pageSize uint64 = 5
	for cursor < 25 {
		txIDs, err := vm.addressTxsIndexer.Read(addr[:], assetID, cursor, pageSize)
		assert.NoError(t, err)
		assert.Len(t, txIDs, 5)
		assert.Equal(t, txIDs, testTxs[cursor:cursor+pageSize])
		cursor += pageSize
	}
}

func TestIndexingNewInitWithIndexingEnabled(t *testing.T) {
	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	ctx := NewContext(t)

	db := baseDBManager.NewPrefixDBManager([]byte{1}).Current().Database

	// start with indexing enabled
	_, err := index.NewIndexer(db, ctx.Log, "", prometheus.NewRegistry(), true)
	assert.NoError(t, err)

	// now disable indexing with allow-incomplete set to false
	_, err = index.NewNoIndexer(db, false)
	assert.Error(t, err)

	// now disable indexing with allow-incomplete set to true
	_, err = index.NewNoIndexer(db, true)
	assert.NoError(t, err)
}

func TestIndexingNewInitWithIndexingDisabled(t *testing.T) {
	ctx := NewContext(t)
	db := memdb.New()

	// disable indexing with allow-incomplete set to false
	_, err := index.NewNoIndexer(db, false)
	assert.NoError(t, err)

	// It's not OK to have an incomplete index when allowIncompleteIndices is false
	_, err = index.NewIndexer(db, ctx.Log, "", prometheus.NewRegistry(), false)
	assert.Error(t, err)

	// It's OK to have an incomplete index when allowIncompleteIndices is true
	_, err = index.NewIndexer(db, ctx.Log, "", prometheus.NewRegistry(), true)
	assert.NoError(t, err)

	// It's OK to have an incomplete index when indexing currently disabled
	_, err = index.NewNoIndexer(db, false)
	assert.NoError(t, err)

	// It's OK to have an incomplete index when allowIncompleteIndices is true
	_, err = index.NewNoIndexer(db, true)
	assert.NoError(t, err)
}

func TestIndexingAllowIncomplete(t *testing.T) {
	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	ctx := NewContext(t)

	prefixDB := baseDBManager.NewPrefixDBManager([]byte{1}).Current().Database
	db := versiondb.New(prefixDB)
	// disabled indexer will persist idxEnabled as false
	_, err := index.NewNoIndexer(db, false)
	assert.NoError(t, err)

	// we initialise with indexing enabled now and allow incomplete indexing as false
	_, err = index.NewIndexer(db, ctx.Log, "", prometheus.NewRegistry(), false)
	// we should get error because:
	// - indexing was disabled previously
	// - node now is asked to enable indexing with allow incomplete set to false
	assert.Error(t, err)
}

func buildPlatformUTXO(utxoID avax.UTXOID, txAssetID avax.Asset, addr ids.ShortID) *avax.UTXO {
	return &avax.UTXO{
		UTXOID: utxoID,
		Asset:  txAssetID,
		Out: &secp256k1fx.TransferOutput{
			Amt: 1000,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
}

func signTX(codec codec.Manager, tx *Tx, key *crypto.PrivateKeySECP256K1R) error {
	return tx.SignSECP256K1Fx(codec, [][]*crypto.PrivateKeySECP256K1R{{key}})
}

func buildTX(utxoID avax.UTXOID, txAssetID avax.Asset, address ...ids.ShortID) *Tx {
	return &Tx{
		UnsignedTx: &BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				Ins: []*avax.TransferableInput{{
					UTXOID: utxoID,
					Asset:  txAssetID,
					In: &secp256k1fx.TransferInput{
						Amt:   1000,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []*avax.TransferableOutput{{
					Asset: txAssetID,
					Out: &secp256k1fx.TransferOutput{
						Amt: 1000,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     address,
						},
					},
				}},
			},
		},
	}
}

func setupTestVM(t *testing.T, ctx *snow.Context, baseDBManager manager.Manager, genesisBytes []byte, issuer chan common.Message, config Config) *VM {
	vm := &VM{}
	avmConfigBytes, err := BuildAvmConfigBytes(config)
	assert.NoError(t, err)
	appSender := &common.SenderTest{}
	if err := vm.Initialize(
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		avmConfigBytes,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
		appSender,
	); err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	if err := vm.SetState(snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}
	return vm
}

func assertLatestIdx(t *testing.T, db database.Database, sourceAddress ids.ShortID, assetID ids.ID, expectedIdx uint64) {
	addressDB := prefixdb.New(sourceAddress[:], db)
	assetDB := prefixdb.New(assetID[:], addressDB)

	expectedIdxBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(expectedIdxBytes, expectedIdx)

	idxBytes, err := assetDB.Get([]byte("idx"))
	assert.NoError(t, err)

	assert.EqualValues(t, expectedIdxBytes, idxBytes)
}

func checkIndexedTX(db database.Database, index uint64, sourceAddress ids.ShortID, assetID ids.ID, transactionID ids.ID) error {
	addressDB := prefixdb.New(sourceAddress[:], db)
	assetDB := prefixdb.New(assetID[:], addressDB)

	idxBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(idxBytes, index)
	tx1Bytes, err := assetDB.Get(idxBytes)
	if err != nil {
		return err
	}

	var txID ids.ID
	copy(txID[:], tx1Bytes)

	if txID != transactionID {
		return fmt.Errorf("txID %s not same as %s", txID, transactionID)
	}
	return nil
}

func assertIndexedTX(t *testing.T, db database.Database, index uint64, sourceAddress ids.ShortID, assetID ids.ID, transactionID ids.ID) {
	if err := checkIndexedTX(db, index, sourceAddress, assetID, transactionID); err != nil {
		t.Fatal(err)
	}
}

// Sets up test tx IDs in DB in the following structure for the indexer to pick them up:
// [address] prefix DB
//		[assetID] prefix DB
//			- "idx": 2
//			- 0: txID1
//			- 1: txID1
func setupTestTxsInDB(t *testing.T, db *versiondb.Database, address ids.ShortID, assetID ids.ID, txCount int) []ids.ID {
	var testTxs []ids.ID
	for i := 0; i < txCount; i++ {
		testTxs = append(testTxs, ids.GenerateTestID())
	}

	addressPrefixDB := prefixdb.New(address[:], db)
	assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)
	var idx uint64
	idxBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(idxBytes, idx)
	for _, txID := range testTxs {
		txID := txID
		err := assetPrefixDB.Put(idxBytes, txID[:])
		assert.NoError(t, err)
		idx++
		binary.BigEndian.PutUint64(idxBytes, idx)
	}
	_, err := db.CommitBatch()
	assert.NoError(t, err)

	err = assetPrefixDB.Put([]byte("idx"), idxBytes)
	assert.NoError(t, err)
	err = db.Commit()
	assert.NoError(t, err)
	return testTxs
}
