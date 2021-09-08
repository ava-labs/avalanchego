// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/stretchr/testify/assert"
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

// shows that a CreateChainTx received as gossip response can be added to
// mempool and then remove by inclusion in a block
func TestMempool_Add_Gossiped_CreateChainTx(t *testing.T) {
	assert := assert.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.blockBuilder

	// create tx to be gossiped
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	// gossip tx and check it is accepted
	nodeID := ids.GenerateTestShortID()
	msg := message.Tx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	vm.requestID++
	vm.requestsContent[vm.requestID] = txID
	err = vm.AppResponse(nodeID, vm.requestID, msgBytes)
	assert.NoError(err)

	has := mempool.Has(txID)
	assert.True(has, "valid tx not recorded into mempool")

	// show that build block include that tx and removes it from mempool
	blkIntf, err := vm.BuildBlock()
	assert.NoError(err)

	blk, ok := blkIntf.(*StandardBlock)
	assert.True(ok, "expected standard block")
	assert.Len(blk.Txs, 1, "standard block should include a single transaction")
	assert.Equal(txID, blk.Txs[0].ID(), "standard block does not include expected transaction")

	has = mempool.Has(txID)
	assert.False(has, "tx included in block is still recorded into mempool")
}

// show that a tx discovered by a GossipResponse is re-gossiped only if added to
// the mempool
func TestMempool_AppResponseHandling(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.blockBuilder.Mempool.(*mempool)

	var (
		wasGossiped   bool
		gossipedBytes []byte
	)
	sender.SendAppGossipF = func(b []byte) error {
		wasGossiped = true
		gossipedBytes = b
		return nil
	}

	// create tx to be received from AppGossipResponse
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	// responses with unknown requestID are rejected
	nodeID := ids.GenerateTestShortID()
	msg := message.Tx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err, "could not encode tx")

	err = vm.AppResponse(nodeID, 0, msgBytes)
	assert.NoError(err, "responses with unknown requestID should be dropped")

	has := mempool.Has(txID)
	assert.False(has, "responses with unknown requestID should not affect mempool")
	assert.False(wasGossiped, "responses with unknown requestID should not result in gossiping")

	vm.requestID++
	vm.requestsContent[vm.requestID] = txID

	// receive tx and check that it is accepted and re-gossiped
	err = vm.AppResponse(nodeID, vm.requestID, msgBytes)
	assert.NoError(err, "error in reception of tx")

	has = mempool.Has(txID)
	assert.True(has, "valid tx not recorded into mempool")
	assert.True(wasGossiped, "tx accepted in mempool should have been re-gossiped")

	// show that gossiped bytes can be duly decoded
	replyIntf, err := message.Parse(gossipedBytes)
	assert.NoError(err)

	reply, ok := replyIntf.(*message.TxNotify)
	assert.True(ok)
	assert.Equal(txID, reply.TxID)

	// show that if the tx is not re-gossiped if it was already known about
	wasGossiped = false
	err = vm.AppResponse(nodeID, vm.requestID, msgBytes)
	assert.NoError(err, "error in reception of tx")
	assert.False(wasGossiped, "unaccepted tx should have not been re-gossiped")

	// show that if the tx is not re-gossiped if the mempool was full
	tx2, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		avm.ID,
		nil,
		"chain name 2",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)
	txID2 := tx2.ID()

	msg = message.Tx{
		Tx: tx2.Bytes(),
	}
	msgBytes, err = message.Build(&msg)
	assert.NoError(err)

	vm.requestID++
	vm.requestsContent[vm.requestID] = txID2

	mempool.totalBytesSize = maxMempoolSize
	err = vm.AppResponse(nodeID, vm.requestID, msgBytes)
	assert.NoError(err, "error in reception of tx")
	assert.False(wasGossiped, "unaccepted tx should have not been re-gossiped")
}

// show that invalid txs are not accepted to mempool, nor rejected
func TestMempool_AppResponseHandling_InvalidTx(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	var wasGossiped bool
	sender.SendAppGossipF = func(b []byte) error {
		wasGossiped = true
		return nil
	}

	txBytes := utils.RandomBytes(100)
	txID := ids.GenerateTestID()

	// gossip tx and check it is dropped
	nodeID := ids.GenerateTestShortID()
	msg := message.Tx{
		Tx: txBytes,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err, "could not encode tx")

	vm.requestID++
	vm.requestsContent[vm.requestID] = txID

	err = vm.AppResponse(nodeID, vm.requestID, msgBytes)
	assert.NoError(err, "error in reception of gossiped tx")
	assert.False(wasGossiped, "invalid tx should not be re-gossiped")
}

// show that a txID discovered from gossip is requested to the same node only if
// the txID is unknown
func TestMempool_AppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	nodeID := ids.GenerateTestShortID()

	var (
		wasRequested       bool
		wasRequestedByNode bool
		requestedBytes     []byte
	)
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) error {
		wasRequested = true
		if nodes.Contains(nodeID) {
			wasRequestedByNode = true
		}
		requestedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	txNotify := message.TxNotify{
		TxID: txID,
	}
	txNotifyBytes, err := message.Build(&txNotify)
	assert.NoError(err)

	// show that unknown txID is requested
	err = vm.AppGossip(nodeID, txNotifyBytes)
	assert.NoError(err, "error in reception of gossiped tx")
	assert.True(wasRequested, "unknown txID should have been requested")
	assert.True(wasRequestedByNode, "unknown txID should have been requested to the same node")

	// show that requested bytes can be decoded correctly
	replyIntf, err := message.Parse(requestedBytes)
	assert.NoError(err, "requested bytes following gossiping cannot be decoded")

	reply, ok := replyIntf.(*message.TxNotify)
	assert.True(ok, "wrong message sent")
	assert.Equal(txID, reply.TxID, "wrong txID requested")

	msg := message.Tx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	var wasGossiped bool
	sender.SendAppGossipF = func([]byte) error {
		wasGossiped = true
		return nil
	}

	err = vm.AppResponse(nodeID, vm.requestID, msgBytes)
	assert.NoError(err, "error in reception of tx")

	assert.True(wasGossiped, "new tx should have been gossiped")
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempool_AppGossipHandling_InvalidTx(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.blockBuilder

	var wasRequested bool
	sender.SendAppRequestF = func(ids.ShortSet, uint32, []byte) error {
		wasRequested = true
		return nil
	}

	// create a tx and mark as invalid
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	mempool.MarkDropped(txID)

	// show that the invalid tx is not requested
	nodeID := ids.GenerateTestShortID()
	msg := message.TxNotify{
		TxID: txID,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err, "error in reception of gossiped tx")
	assert.False(wasRequested, "rejected txs should not be requested")
}

// show that a node answers to a request with a response if it has the requested
// tx
func TestMempool_AppRequestHandling(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.blockBuilder

	var (
		wasResponded   bool
		respondedBytes []byte
	)
	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
		wasResponded = true
		respondedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	msg := message.TxNotify{
		TxID: txID,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	// show that there is no response if tx is unknown
	nodeID := ids.GenerateTestShortID()
	err = vm.AppRequest(nodeID, 0, msgBytes)
	assert.NoError(err, "error in request of tx")
	assert.False(wasResponded, "there should be no response with an unknown tx")

	// show that there is response if tx is known
	err = mempool.AddUncheckedTx(tx)
	assert.NoError(err, "could not add tx to mempool")

	err = vm.AppRequest(nodeID, 0, msgBytes)
	assert.NoError(err, "error in request of tx")
	assert.True(wasResponded, "there should be a response with a known tx")

	replyIntf, err := message.Parse(respondedBytes)
	assert.NoError(err, "error parsing response")

	reply, ok := replyIntf.(*message.Tx)
	assert.True(ok, "unknown response message")
	assert.Equal(tx.Bytes(), reply.Tx, "wrong tx provided")
}

// show that locally generated txs are gossiped
func TestMempool_IssueTxAndGossiping(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.blockBuilder

	var gossipedBytes []byte
	sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// add a tx to the mempool
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	err := mempool.IssueTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")

	replyIntf, err := message.Parse(gossipedBytes)
	assert.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.TxNotify)
	assert.True(ok, "unknown message type")
	assert.Equal(txID, reply.TxID, "wrong txID gossiped")
}
