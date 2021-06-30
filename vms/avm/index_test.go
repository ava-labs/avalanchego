// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/binary"
	"testing"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/stretchr/testify/assert"
)

func TestIndexTransaction_Ordered(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewDefaultMemDBManager()

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	if err != nil {
		t.Fatal(err)
	}

	ctx := NewContext(t)
	ctx.SharedMemory = m.NewSharedMemory(chainID)
	peerSharedMemory := m.NewSharedMemory(platformChainID)

	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()
	vm := setupTestVM(t, ctx, baseDBManager, genesisBytes, issuer)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	// get the key
	key := keys[0]

	var uniqueTxs []*UniqueTx
	txAssetID := avax.Asset{ID: avaxID}

	ctx.Lock.Lock()
	for i := 0; i < 5; i++ {
		// create utxoID and assetIDs
		utxoID := avax.UTXOID{
			TxID: ids.GenerateTestID(),
		}

		// build the transaction
		tx := buildTX(utxoID, txAssetID, key.PublicKey().Address())

		// sign the transaction
		if err := signTX(vm.codec, tx, key); err != nil {
			t.Fatal(err)
		}

		// Provide the platform UTXO
		utxo := buildPlatformUTXO(utxoID, txAssetID, key)

		utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
		if err != nil {
			t.Fatal(err)
		}

		// save utxo to state
		inputID := utxo.InputID()
		if err := vm.state.PutUTXO(inputID, utxo); err != nil {
			t.Fatal("Error saving utxo", err)
		}

		// put utxo in shared memory
		if err := peerSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				key.PublicKey().Address().Bytes(),
			},
		}}); err != nil {
			t.Fatal(err)
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

		// index the transaction
		err = vm.addressTxsIndexer.AddUTXOIDs(vm, uniqueParsedTX.inputUTXOs)
		assert.NoError(t, err)
		vm.addressTxsIndexer.AddUTXOs(uniqueParsedTX.UTXOs())
		err = vm.addressTxsIndexer.Write(uniqueParsedTX.txID)
		assert.NoError(t, err)
	}

	// ensure length is 5
	assert.Len(t, uniqueTxs, 5)
	// for each *UniqueTx check its indexed at right index
	for i, tx := range uniqueTxs {
		assertIndexedTX(t, vm.db, uint64(i), key.PublicKey().Address(), txAssetID.ID, tx.ID())
	}

	assertLatestIdx(t, vm.db, key.PublicKey().Address(), txAssetID.ID, 5)
}

func TestIndexTransaction_MultipleAddresses(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewDefaultMemDBManager()

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	if err != nil {
		t.Fatal(err)
	}

	ctx := NewContext(t)
	ctx.SharedMemory = m.NewSharedMemory(chainID)
	peerSharedMemory := m.NewSharedMemory(platformChainID)

	genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)

	avaxID := genesisTx.ID()
	vm := setupTestVM(t, ctx, baseDBManager, genesisBytes, issuer)
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
		// create utxoID and assetIDs
		utxoID := avax.UTXOID{
			TxID: ids.GenerateTestID(),
		}

		// build the transaction
		tx := buildTX(utxoID, txAssetID, key.PublicKey().Address())

		// sign the transaction
		if err := signTX(vm.codec, tx, key); err != nil {
			t.Fatal(err)
		}

		// Provide the platform UTXO
		utxo := buildPlatformUTXO(utxoID, txAssetID, key)

		utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
		if err != nil {
			t.Fatal(err)
		}

		// save utxo to state
		inputID := utxo.InputID()
		if err := vm.state.PutUTXO(inputID, utxo); err != nil {
			t.Fatal("Error saving utxo", err)
		}

		// put utxo in shared memory
		if err := peerSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				key.PublicKey().Address().Bytes(),
			},
		}}); err != nil {
			t.Fatal(err)
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
		addressTxMap[key.PublicKey().Address()] = uniqueParsedTX

		// index the transaction
		err = vm.addressTxsIndexer.AddUTXOIDs(vm, uniqueParsedTX.InputUTXOs())
		assert.NoError(t, err)
		vm.addressTxsIndexer.AddUTXOs(uniqueParsedTX.UTXOs())
		err = vm.addressTxsIndexer.Write(uniqueParsedTX.txID)
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
	vm.addressTxsIndexer = NewAddressTxsIndexer(vm.db, vm.ctx.Log, vm.metrics)
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
	var cursor uint64 = 0
	var pageSize uint64 = 5
	for cursor < 25 {
		txIDs, err := vm.addressTxsIndexer.Read(addr, assetID, cursor, pageSize)
		assert.NoError(t, err)
		assert.Len(t, txIDs, 5)
		assert.Equal(t, txIDs, testTxs[cursor:cursor+pageSize])
		cursor += pageSize
	}
}

func buildPlatformUTXO(utxoID avax.UTXOID, txAssetID avax.Asset, key *crypto.PrivateKeySECP256K1R) *avax.UTXO {
	return &avax.UTXO{
		UTXOID: utxoID,
		Asset:  txAssetID,
		Out: &secp256k1fx.TransferOutput{
			Amt: 1000,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{key.PublicKey().Address()},
			},
		},
	}
}

func signTX(codec codec.Manager, tx *Tx, key *crypto.PrivateKeySECP256K1R) error {
	return tx.SignSECP256K1Fx(codec, [][]*crypto.PrivateKeySECP256K1R{{key}})
}

func buildTX(utxoID avax.UTXOID, txAssetID avax.Asset, address ids.ShortID) *Tx {
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
							Addrs:     []ids.ShortID{address},
						},
					},
				}},
			},
		},
	}
}

func setupTestVM(t *testing.T, ctx *snow.Context, baseDBManager manager.Manager, genesisBytes []byte, issuer chan common.Message) *VM {
	vm := &VM{}
	if err := vm.Initialize(
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		BuildAvmConfigBytes(),
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
	); err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
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

func assertIndexedTX(t *testing.T, db database.Database, index uint64, sourceAddress ids.ShortID, assetID ids.ID, transactionID ids.ID) {
	addressDB := prefixdb.New(sourceAddress[:], db)
	assetDB := prefixdb.New(assetID[:], addressDB)

	idxBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(idxBytes, index)
	tx1Bytes, err := assetDB.Get(idxBytes)
	assert.NoError(t, err)

	var txID ids.ID
	copy(txID[:], tx1Bytes)

	if txID != transactionID {
		t.Fatalf("txID %s not same as %s", txID, transactionID)
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
	var idx uint64 = 0
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
	return testTxs
}
