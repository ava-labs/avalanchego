// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

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

func TestIndexTransaction(t *testing.T) {
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
	ctx.Lock.Lock()
	vm := &VM{}
	if err := vm.Initialize(
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		nil,
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

	key := keys[0] // generate multiple transactions for multiple addresses to verify

	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	txAssetID := avax.Asset{ID: avaxID}
	tx := &Tx{UnsignedTx: &ImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		SourceChain: platformChainID,
		ImportedIns: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  txAssetID,
			In: &secp256k1fx.TransferInput{
				Amt:   1000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	}}
	if err := tx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	// Provide the platform UTXO:

	utxo := &avax.UTXO{
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

	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	inputID := utxo.InputID()

	if err := vm.state.PutUTXO(inputID, utxo); err != nil {
		t.Fatal("Error saving utxo", err)
	}

	if err := peerSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			key.PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	if _, err := vm.IssueTx(tx.Bytes()); err != nil {
		t.Fatalf("should have issued the transaction correctly but errored: %s", err)
	}
	ctx.Lock.Unlock()

	msg := <-issuer
	if msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	txs := vm.PendingTxs()
	if len(txs) != 1 {
		t.Fatalf("Should have returned %d tx(s)", 1)
	}

	parsedTx := txs[0]

	err = IndexTransaction(parsedTx.(*UniqueTx))
	assert.NoError(t, err)

	assertIndex(t, key.PublicKey().Address(), txAssetID.ID, tx.ID(), vm.db)
}
