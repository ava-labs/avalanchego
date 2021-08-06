package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/avm"
)

func getTheValidTx(vm *VM, t *testing.T) *Tx {
	res, err := vm.newCreateChainTx(
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

	return res
}

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
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	// add a tx to it
	tx := getTheValidTx(vm, t)
	if err := mempool.IssueTx(tx); err != nil {
		t.Fatal("Could not add tx to mempool")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and removes it from mempool
	blk, err := vm.BuildBlock()
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
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	// create tx to be gossiped
	tx := getTheValidTx(vm, t)

	// gossip tx and check it is accepted
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppResponse(nodeID, vm.IssueID(), tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and removes it from mempool
	blk, err := vm.BuildBlock()
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
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	// create candidate tx
	tx := getTheValidTx(vm, t)

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
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	isTxReGossiped := false
	var gossipedBytes []byte
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) error {
		isTxReGossiped = true
		gossipedBytes = b
		return nil
	}

	// create tx to be received from AppGossipResponse
	tx := getTheValidTx(vm, t)

	// responses with unknown requestID are rejected
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	reqID := vm.IssueID()

	unknownReqID := reqID + 1
	if err := vm.AppResponse(nodeID, unknownReqID, tx.Bytes()); err != nil {
		t.Fatal("responses with unknown requestID should be dropped")
	}
	if mempool.has(tx.ID()) {
		t.Fatal("responses with unknown requestID should not affect mempool")
	}
	if isTxReGossiped {
		t.Fatal("responses with unknown requestID should not result in gossiping")
	}

	// received tx and check it is accepted and re-gossiped
	if err := vm.AppResponse(nodeID, reqID, tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}
	if !isTxReGossiped {
		t.Fatal("tx accepted in mempool should have been re-gossiped")
	}

	// show that gossiped bytes can be duly decoded
	if err := vm.AppGossip(nodeID, gossipedBytes); err != nil {
		t.Fatal("bytes gossiped following AppResponse are not valid")
	}

	// show that if tx is not accepted to mempool is not regossiped

	// case 1: reinsertion attempt
	isTxReGossiped = false
	if err := vm.AppResponse(nodeID, vm.IssueID(), tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxReGossiped {
		t.Fatal("unaccepted tx should have not been regossiped")
	}

	// case 2: filled mempool
	isTxReGossiped = false
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
	if err := vm.AppResponse(nodeID, vm.IssueID(), tx2.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxReGossiped {
		t.Fatal("unaccepted tx should have not been regossiped")
	}
}

func TestMempool_AppResponseHandling_InvalidTx(t *testing.T) {
	// show that invalid txes are not accepted to mempool, nor rejected

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	isTxReGossiped := false
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func([]byte) error {
		isTxReGossiped = true
		return nil
	}

	// create an invalid tx
	illFormedTx, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		ids.Empty,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err == nil {
		t.Fatal("test requires invalid tx")
	}

	// gossip tx and check it is accepted and re-gossiped
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	reqID := vm.IssueID()
	if err := vm.AppResponse(nodeID, reqID, illFormedTx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if mempool.has(illFormedTx.ID()) {
		t.Fatal("invalid tx should not be accepted to mempool")
	}
	if isTxReGossiped {
		t.Fatal("invalid tx should not be re-gossiped")
	}
	if !mempool.isAlreadyRejected(illFormedTx.ID()) {
		t.Fatal("invalid tx should be marked as rejected")
	}
}

func TestMempool_AppGossipHandling(t *testing.T) {
	// show that a txID discovered from gossip is requested to the same node
	// only if the txID is unknown

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	isTxRequested := false
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	IsRightNodeRequested := false
	var requestedBytes []byte
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) error {
		isTxRequested = true
		if nodes.Contains(nodeID) {
			IsRightNodeRequested = true
		}
		requestedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, t)
	txID, err := vm.codec.Marshal(codecVersion, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that unknown txID is requested
	if err := vm.AppGossip(nodeID, txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isTxRequested {
		t.Fatal("unknown txID should have been requested")
	}
	if !IsRightNodeRequested {
		t.Fatal("unknown txID should have been requested to a different node")
	}

	// show that requested bytes can be duly decoded
	if err := vm.AppRequest(nodeID, vm.IssueID(), requestedBytes); err != nil {
		t.Fatal("requested bytes following gossiping cannot be decoded")
	}

	// show that known txID is not requested
	isTxRequested = false
	if err := mempool.AddUncheckedTx(tx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppGossip(nodeID, txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("known txID should not be requested")
	}
}

func TestMempool_AppGossipHandling_InvalidTx(t *testing.T) {
	// show that txes already marked as invalid are not re-requested on gossiping

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	isTxRequested := false
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(ids.ShortSet, uint32, []byte) error {
		isTxRequested = true
		return nil
	}

	// create a tx and mark as invalid
	rejectedTx := getTheValidTx(vm, t)
	if err := mempool.markReject(rejectedTx); err != nil {
		t.Fatal("could not mark tx as rejected")
	}

	// show that the invalid tx is not requested
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	rejectedTxID, err := vm.codec.Marshal(codecVersion, rejectedTx.ID())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.AppGossip(nodeID, rejectedTxID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("rejected txs should not be requested")
	}
}

func TestMempool_AppRequestHandling(t *testing.T) {
	// show that a node answer to request with response
	// only if it has the requested tx

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	isResponseIssued := false
	var respondedBytes []byte
	sender.CantSendAppResponse = true
	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
		isResponseIssued = true
		respondedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, t)
	txID, err := vm.codec.Marshal(codecVersion, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that there is no response if tx is unknown
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppRequest(nodeID, vm.IssueID(), txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isResponseIssued {
		t.Fatal("there should be no response with unknown tx")
	}

	// show that there is response if tx is unknown
	if err := mempool.AddUncheckedTx(tx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppRequest(nodeID, vm.IssueID(), txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isResponseIssued {
		t.Fatal("there should be a response with known tx")
	}

	// show that responded bytes can be duly decoded
	if err := vm.AppResponse(nodeID, vm.IssueID(), respondedBytes); err != nil {
		t.Fatal("bytes sent in response of AppRequest cannot be decoded")
	}
}

func TestMempool_AppRequestHandling_InvalidTx(t *testing.T) {
	// should a node issue a request for rejecte tx
	// (which should not have been gossiped around in the first place)
	// no response is sent

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	isResponseIssued := false
	sender.CantSendAppResponse = true
	sender.SendAppResponseF = func(ids.ShortID, uint32, []byte) error {
		isResponseIssued = true
		return nil
	}

	// create a tx
	rejectedTx := getTheValidTx(vm, t)
	rejectedTxID, err := vm.codec.Marshal(codecVersion, rejectedTx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that there is no response if tx is rejected
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppRequest(nodeID, vm.IssueID(), rejectedTxID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isResponseIssued {
		t.Fatal("there should be no response with unknown tx")
	}
}

func TestMempool_IssueTxAndGossiping(t *testing.T) {
	// show that locally generated txes are gossiped
	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.mempool

	gossipedBytes := make([]byte, 0)
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// add a tx to it
	tx := getTheValidTx(vm, t)
	if err := mempool.IssueTx(tx); err != nil {
		t.Fatal("Could not add tx to mempool")
	}
	if len(gossipedBytes) == 0 {
		t.Fatal("expected call to SendAppGossip not issued")
	}

	// check that gossiped bytes can be requested
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppGossip(nodeID, gossipedBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
}
