package evm

import (
	"crypto/rand"
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
)

func TestAcceptSubscription(t *testing.T) {
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := coreth.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Import 1 AVAX
	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm1.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm1.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory1 := sharedMemory1.NewSharedMemory(vm1.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory1.Put(vm1.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	ch := make(chan core.ChainEvent)
	events := make([]core.ChainEvent, 0, 10)
	chdone := make(chan struct{})
	go func() {
		for {
			select {
			case ev := <-ch:
				events = append(events, ev)
			case <-chdone:
				return
			}
		}
	}()
	vm1.Chain().BlockChain().SubscribeChainAcceptedEvent(ch)

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, key.Address, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatal(err)
	}

	chdone <- struct{}{}
	close(ch)

	if len(events) < 1 {
		t.Fatalf("no events")
	}
	if events[0].Block.NumberU64() != 1 {
		t.Fatalf("no accepted block 1")
	}
}
