package platformvm

import (
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/avm"
)

func TestMempool_Add_LocallyCreate_CreateChainTx(t *testing.T) {
	// shows that a locally generated CreateChainTx can be added to mempool
	// and then removed by inclusion in a block

	vm, _, _ := defaultVM()
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
	if err := mempool.IssueTx(tx); err != nil {
		t.Fatal("Could not add tx to mempool")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and removes it from mempool
	blk, err := mempool.BuildBlock()
	if err != nil {
		t.Fatal("could not build block out of mempool")
	}

	stdBlk, ok := blk.(*StandardBlock)
	if !ok {
		t.Fatal("expected standard block")
	}
	if len(stdBlk.Txs) != 1 {
		t.Fatal("standard block should include a single transaction")
	}
	if stdBlk.Txs[0].ID() != tx.ID() {
		t.Fatal("standard block does not include expected transaction")
	}

	if mempool.has(tx.ID()) {
		t.Fatal("tx included in block is still recorded into mempool")
	}
}

func TestMempool_Add_Gossiped_CreateChainTx(t *testing.T) {
	// shows that a CreateChainTx received as gossip response can be added to mempool
	// and then remove by inclusion in a block

	vm, _, _ := defaultVM()
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
	dummyNodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppResponse(dummyNodeID, vm.issueRequestID(), tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and removes it from mempool
	blk, err := mempool.BuildBlock()
	if err != nil {
		t.Fatal("could not build block out of mempool")
	}

	stdBlk, ok := blk.(*StandardBlock)
	if !ok {
		t.Fatal("expected standard block")
	}
	if len(stdBlk.Txs) != 1 {
		t.Fatal("standard block should include a single transaction")
	}
	if stdBlk.Txs[0].ID() != tx.ID() {
		t.Fatal("standard block does not include expected transaction")
	}

	if mempool.has(tx.ID()) {
		t.Fatal("tx included in block is still recorded into mempool")
	}
}

func TestMempool_MaxMempoolSizeHandling(t *testing.T) {
	// shows that valid tx is not added to mempool if this would exceed its maximum size

	vm, _, _ := defaultVM()
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
	mempool.totalBytesSize = MaxMempoolByteSize - len(tx.Bytes()) + 1

	if err := mempool.AddUncheckedTx(tx); err != errTxExceedingMempoolSize {
		t.Fatal("max mempool size breached")
	}

	// shortcut to simulated almost filled mempool
	mempool.totalBytesSize = MaxMempoolByteSize - len(tx.Bytes())

	if err := mempool.AddUncheckedTx(tx); err != nil {
		t.Fatal("should be possible to add tx")
	}
}

func TestMempool_AppResponseHandling(t *testing.T) {
	// show that a tx discovered by a GossipResponse is re-gossiped
	// only if duly added to mempool

	vm, _, sender := defaultVM()
	isTxRequested := false
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) { isTxRequested = true }
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	mempool := &vm.mempool

	// create tx to be received from AppGossipResponse
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

	// gossip tx and check it is accepted and re-gossiped
	dummyNodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	reqID := vm.issueRequestID()
	if err := vm.AppResponse(dummyNodeID, reqID, tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}
	if !isTxRequested {
		t.Fatal("tx accepted in mempool should have been re-gossiped")
	}

	// show that if tx is not accepted to mempool is not regossiped

	// case 1: reinsertion attempt
	isTxRequested = false
	if err := vm.AppResponse(dummyNodeID, vm.issueRequestID(), tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("unaccepted tx should have not been regossiped")
	}

	// case 2: filled mempool
	isTxRequested = false
	tx2, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		avm.ID,
		nil,
		"chain name 2",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.mempool.totalBytesSize = MaxMempoolByteSize
	if err := vm.AppResponse(dummyNodeID, vm.issueRequestID(), tx2.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("unaccepted tx should have not been regossiped")
	}

	// TODO: case 3: received invalid tx
}

func TestMempool_AppGossipHandling(t *testing.T) {
	// show that a txID discovered from gossip is requested to the same node
	// only if the txID is unknown

	vm, _, sender := defaultVM()
	isTxRequested := false
	NodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	IsRightNodeRequested := false
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) {
		isTxRequested = true
		if nodes.Contains(NodeID) {
			IsRightNodeRequested = true
		}
	}
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	mempool := &vm.mempool

	// create a tx
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
	txID, err := vm.codec.Marshal(codecVersion, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that unknown txID is requested
	if err := vm.AppGossip(NodeID, txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isTxRequested {
		t.Fatal("unknown txID should have been requested")
	}
	if !IsRightNodeRequested {
		t.Fatal("unknown txID should have been requested to a different node")
	}

	// show that known txID is not requested
	isTxRequested = false
	if err := mempool.AddUncheckedTx(tx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppGossip(NodeID, txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("known txID should not be requested")
	}
}

func TestMempool_AppRequestHandling(t *testing.T) {
	// show that a node answer to request with response
	// only if it has the requested tx

	vm, _, sender := defaultVM()
	isResponseIssued := false
	sender.CantSendAppResponse = true
	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) {
		isResponseIssued = true
	}
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	mempool := &vm.mempool

	// create a tx
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
	txID, err := vm.codec.Marshal(codecVersion, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that there is no response if tx is unknown
	dummyNodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppRequest(dummyNodeID, vm.issueRequestID(), txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isResponseIssued {
		t.Fatal("there should be no response with unknown tx")
	}

	// show that there is response if tx is unknown
	if err := mempool.AddUncheckedTx(tx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppRequest(dummyNodeID, vm.issueRequestID(), txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isResponseIssued {
		t.Fatal("there should be a response with known tx")
	}
}

func TestMempool_AppRequestIDHandling(t *testing.T) {
	// show that multiple outstanding requests can be issued, with increasing reqID
	// response can arrive in any order. Requests with unknown reqID are dropped

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// show that reqID increase starting from zero
	issueRounds := []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, i := range issueRounds {
		currentID := vm.issueRequestID()
		if currentID != i {
			t.Fatal("unexpected reqID")
		}
	}

	// show that unknown reqID results in error
	unknownID := uint32(len(issueRounds)) + 1
	if err := vm.consumeReqID(unknownID); err == nil {
		t.Fatalf("should not be possible to consume unknownID")
	}

	// show response can come in any order
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(issueRounds), func(i, j int) {
		issueRounds[i], issueRounds[j] = issueRounds[j], issueRounds[i]
	})

	for it, i := range issueRounds {
		if err := vm.consumeReqID(i); err != nil {
			t.Fatalf("consume: err %v at iteration %d, index %d, permutation %v", err, it, i, issueRounds)
		}

		// re-consuming same reqID should fail
		if err := vm.consumeReqID(i); err == nil {
			t.Fatalf("re-consume: err %v at iteration %d, index %d, permutation %v", err, it, i, issueRounds)
		}
	}

	// show that consumed reqIDs are recycled
	firstID := vm.issueRequestID()
	secondID := vm.issueRequestID()
	thirdID := vm.issueRequestID()

	if err := vm.consumeReqID(secondID); err != nil {
		t.Fatalf("could not consume secondID")
	}
	if err := vm.consumeReqID(thirdID); err != nil {
		t.Fatalf("could not consume thirdID")
	}

	fourthID := vm.issueRequestID()
	if fourthID != secondID {
		t.Fatalf("could not reuse consumed ID")
	}

	if err := vm.consumeReqID(fourthID); err != nil {
		t.Fatalf("could not consume fourthID")
	}
	if err := vm.consumeReqID(firstID); err != nil {
		t.Fatalf("could not consume firstID")
	}

	if lastID := vm.issueRequestID(); lastID != firstID {
		t.Fatalf("could not reuse consumed ID")
	}
}
