package avm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
)

// Test the indexer within the context of a VM
func TestIndexerInVM(t *testing.T) {
	assert := assert.New(t)
	genesisBytes, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	// On startup, there should be nothing in the index
	_, acceptedAnything := vm.indexer.lastAcceptedIndex()
	assert.False(acceptedAnything)
	_, err := vm.indexer.getAcceptedTxRange(0, 1)
	assert.Error(err)
	_, err = vm.indexer.getAcceptedTxByIndex(0)
	assert.Error(err)

	// Create a tx
	avaxTx := GetAVAXTxFromGenesisTest(genesisBytes, t)
	key := keys[0]
	tx0 := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        avaxTx.ID(),
				OutputIndex: 2,
			},
			Asset: avax.Asset{ID: avaxTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: startBalance,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: startBalance - vm.txFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := tx0.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	// Parse, verify and accept this transaction
	tx0Parsed, err := vm.parseTx(tx0.Bytes())
	assert.NoError(err)
	err = tx0Parsed.Verify()
	assert.NoError(err)
	err = tx0Parsed.Accept()
	assert.NoError(err)

	// Ensure the index is correct
	index, acceptedAnything := vm.indexer.lastAcceptedIndex()
	assert.True(acceptedAnything)
	assert.EqualValues(0, index)
	txID, err := vm.indexer.getAcceptedTxByIndex(0)
	assert.NoError(err)
	assert.Equal(txID, tx0Parsed.ID())
	_, err = vm.indexer.getAcceptedTxByIndex(1)
	assert.Error(err)
	txIDs, err := vm.indexer.getAcceptedTxRange(0, 1)
	assert.NoError(err)
	assert.Len(txIDs, 1)
	assert.Contains(txIDs, tx0Parsed.ID())
	txIDs, err = vm.indexer.getAcceptedTxRange(0, 2)
	assert.NoError(err)
	assert.Len(txIDs, 1)
	assert.Contains(txIDs, tx0Parsed.ID())
	_, err = vm.indexer.getAcceptedTxRange(1, 2)
	assert.Error(err)

	// Create another tx
	tx1 := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        tx0Parsed.txID,
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: avaxTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: startBalance - vm.txFee,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: startBalance - 2*vm.txFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := tx1.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	// Parse, verify and accept this transaction
	tx1Parsed, err := vm.parseTx(tx1.Bytes())
	assert.NoError(err)
	err = tx1Parsed.Verify()
	assert.NoError(err)
	err = tx1Parsed.Accept()
	assert.NoError(err)

	// Ensure the index is correct
	index, acceptedAnything = vm.indexer.lastAcceptedIndex()
	assert.True(acceptedAnything)
	assert.EqualValues(1, index)
	txID, err = vm.indexer.getAcceptedTxByIndex(0)
	assert.NoError(err)
	assert.Equal(txID, tx0Parsed.ID())
	txID, err = vm.indexer.getAcceptedTxByIndex(1)
	assert.NoError(err)
	assert.Equal(txID, tx1Parsed.ID())
	_, err = vm.indexer.getAcceptedTxByIndex(2)
	assert.Error(err)
	txIDs, err = vm.indexer.getAcceptedTxRange(0, 2)
	assert.NoError(err)
	assert.Len(txIDs, 2)
	assert.Contains(txIDs, tx0Parsed.ID())
	assert.Contains(txIDs, tx1Parsed.ID())
	txIDs, err = vm.indexer.getAcceptedTxRange(1, 1)
	assert.NoError(err)
	assert.Len(txIDs, 1)
	assert.Contains(txIDs, tx1Parsed.ID())
	txIDs, err = vm.indexer.getAcceptedTxRange(1, 2)
	assert.NoError(err)
	assert.Len(txIDs, 1)
	assert.Contains(txIDs, tx1Parsed.ID())
}
