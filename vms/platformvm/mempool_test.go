package platformvm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/avm"
)

func TestMempool_Add_LocallyCreate_CreateChainTx(t *testing.T) {
	// shows that a locally generated CreateChainTx can be added to mempool
	// and then removed by inclusion in a block

	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	mempool := &vm.mempool

	// add a tx to it
	tx, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		avm.ID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	mempool.IssueTx(tx)
	if err := mempool.IssueTx(tx); err != nil {
		t.Fatal("Could not add tx to mempool")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and removes it from mempool
	if blk, err := mempool.BuildBlock(); err != nil {
		t.Fatal("could not build block out of mempool")
	} else if stdBlk, ok := blk.(*StandardBlock); !ok {
		t.Fatal("expected standard block")
	} else if len(stdBlk.Txs) != 1 {
		t.Fatal("standard block should include a single transaction")
	} else if stdBlk.Txs[0].ID() != tx.ID() {
		t.Fatal("standard block does not include expected transaction")
	}

	if mempool.has(tx.ID()) {
		t.Fatal("tx included in block is still recorded into mempool")
	}
}

func TestMempool_Add_Gossiped_CreateChainTx(t *testing.T) {
	// shows that a CreateChainTx received as gossip response can be added to mempool
	// and then remove by inclusion in a block

	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	mempool := &vm.mempool

	// create tx to be gossiped
	tx, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		avm.ID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// gossip tx and check it is accepted
	dummyNodeID := ids.ShortID{}
	dummyReqID := uint32(1)
	if err := vm.AppResponse(dummyNodeID, dummyReqID, tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and removes it from mempool
	if blk, err := mempool.BuildBlock(); err != nil {
		t.Fatal("could not build block out of mempool")
	} else if stdBlk, ok := blk.(*StandardBlock); !ok {
		t.Fatal("expected standard block")
	} else if len(stdBlk.Txs) != 1 {
		t.Fatal("standard block should include a single transaction")
	} else if stdBlk.Txs[0].ID() != tx.ID() {
		t.Fatal("standard block does not include expected transaction")
	}

	if mempool.has(tx.ID()) {
		t.Fatal("tx included in block is still recorded into mempool")
	}
}

func TestMempool_Add_MaxSize(t *testing.T) {
	// shows that valid tx is not added to mempool if this would exceed its maximum size

	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	mempool := &vm.mempool

	// create candidate tx
	tx, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		avm.ID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// shortcut to simulated almost filled mempool
	mempool.mempoolMetadata.totalBytesSize = MaxMempoolByteSize - len(tx.Bytes()) + 1

	if err := mempool.AddUncheckedTx(tx); err != errTxExceedingMempoolSize {
		t.Fatal("max mempool size breached")
	}

	// shortcut to simulated almost filled mempool
	mempool.mempoolMetadata.totalBytesSize = MaxMempoolByteSize - len(tx.Bytes())

	if err := mempool.AddUncheckedTx(tx); err != nil {
		t.Fatal("should be possible to add tx")
	}
}
