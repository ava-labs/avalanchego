// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/stretchr/testify/assert"
)

func getValidTx(vm *VM, t *testing.T) *Tx {
	res, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
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

// show that a tx learned from gossip is validated and added to mempool
func TestMempoolValidGossipedTxIsAddedToMempool(t *testing.T) {
	assert := assert.New(t)

	vm, _, sender := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	var gossipedBytes []byte
	sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	nodeID := ids.GenerateTestShortID()

	// create a tx
	tx := getValidTx(vm, t)
	txID := tx.ID()

	msg := message.Tx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)
	// Free lock because [AppGossip] waits for the context lock
	vm.ctx.Lock.Unlock()
	// show that unknown tx is added to mempool
	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err, "error in reception of gossiped tx")
	assert.True(vm.mempool.Has(txID))
	// Grab lock back
	vm.ctx.Lock.Lock()

	// and gossiped if it has just been discovered
	assert.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	assert.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.Tx)
	assert.True(ok, "unknown message type")

	retrivedTx := &Tx{}
	_, err = Codec.Unmarshal(reply.Tx, retrivedTx)
	assert.NoError(err, "failed unmarshalling tx")

	unsignedBytes, err := Codec.Marshal(CodecVersion, &retrivedTx.UnsignedTx)
	assert.NoError(err, "failed unmarshalling tx")

	retrivedTx.Initialize(unsignedBytes, reply.Tx)
	assert.Equal(txID, retrivedTx.ID())
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolInvalidGossipedTxIsNotAddedToMempool(t *testing.T) {
	assert := assert.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	// create a tx and mark as invalid
	tx := getValidTx(vm, t)
	txID := tx.ID()
	vm.mempool.MarkDropped(txID)

	// show that the invalid tx is not requested
	nodeID := ids.GenerateTestShortID()
	msg := message.Tx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)
	vm.ctx.Lock.Unlock()
	err = vm.AppGossip(nodeID, msgBytes)
	vm.ctx.Lock.Lock()
	assert.NoError(err, "error in reception of gossiped tx")
	assert.False(vm.mempool.Has(txID))
}

// show that locally generated txs are gossiped
func TestMempoolNewLocaTxIsGossiped(t *testing.T) {
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

	// add a tx to the mempool and show it gets gossiped
	tx := getValidTx(vm, t)
	txID := tx.ID()

	err := mempool.AddUnverifiedTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")
	assert.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	assert.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.Tx)
	assert.True(ok, "unknown message type")

	retrivedTx := &Tx{}
	_, err = Codec.Unmarshal(reply.Tx, retrivedTx)
	assert.NoError(err, "failed unmarshalling tx")

	unsignedBytes, err := Codec.Marshal(CodecVersion, &retrivedTx.UnsignedTx)
	assert.NoError(err, "failed unmarshalling tx")

	retrivedTx.Initialize(unsignedBytes, reply.Tx)
	assert.Equal(txID, retrivedTx.ID())

	// show that transaction is not re-gossiped is recently added to mempool
	gossipedBytes = nil
	vm.mempool.RemoveDecisionTxs([]*Tx{tx})
	err = vm.mempool.AddVerifiedTx(tx)
	assert.NoError(err, "could not reintroduce tx to mempool")

	assert.True(gossipedBytes == nil)
}
