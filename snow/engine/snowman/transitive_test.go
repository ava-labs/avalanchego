// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errUnknownBlock   = errors.New("unknown block")
	errUnknownBytes   = errors.New("unknown bytes")
	errInvalid        = errors.New("invalid")
	errUnexpectedCall = errors.New("unexpected call")
	errTest           = errors.New("non-nil test")

	GenesisID    = ids.GenerateTestID()
	GenesisBytes = utils.RandomBytes(32)
	Genesis      = &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     GenesisID,
			StatusV: choices.Accepted,
		},
		BytesV: GenesisBytes,
	}
)

func BuildChain(root *snowman.TestBlock, length int) []*snowman.TestBlock {
	chain := make([]*snowman.TestBlock, length)
	for i := range chain {
		root = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentV: root.ID(),
			HeightV: root.Height() + 1,
			BytesV:  utils.RandomBytes(32),
		}
		chain[i] = root
	}
	return chain
}

func setup(t *testing.T) (ids.NodeID, validators.Manager, *common.SenderTest, *block.TestVM, *Transitive) {
	require := require.New(t)

	config := DefaultConfig(t)

	vdr := ids.GenerateTestNodeID()
	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, vdr, nil, ids.Empty, 1))
	require.NoError(config.ConnectedValidators.Connected(context.Background(), vdr, version.CurrentApp))
	config.Validators.RegisterSetCallbackListener(config.Ctx.SubnetID, config.ConnectedValidators)

	sender := &common.SenderTest{T: t}
	config.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	config.VM = vm

	snowGetHandler, err := getter.New(
		vm,
		sender,
		config.Ctx.Log,
		time.Second,
		2000,
		config.Ctx.Registerer,
	)
	require.NoError(err)
	config.AllGetsServer = snowGetHandler

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	te, err := New(config)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil
	return vdr, config.Validators, sender, vm, te
}

func TestEngineDropsAttemptToIssueBlockAfterFailedRequest(t *testing.T) {
	require := require.New(t)

	peerID, _, sender, vm, engine := setup(t)

	blks := BuildChain(Genesis, 2)
	parent := blks[0]
	child := blks[1]

	var request *common.Request
	sender.SendGetF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
		require.Nil(request)
		request = &common.Request{
			NodeID:    nodeID,
			RequestID: requestID,
		}
		require.Equal(parent.ID(), blkID)
	}
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(child.Bytes(), b)
		return child, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// Attempting to add [child] will cause [parent] to be requested. While the
	// request for [parent] is outstanding, [child] will be registered into a
	// job blocked on [parent]'s issuance.
	require.NoError(engine.Put(context.Background(), peerID, 0, child.Bytes()))
	require.NotNil(request)
	require.Len(engine.blocked, 1)

	vm.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		return nil, errUnknownBytes
	}

	// Because this request doesn't provide [parent], the [child] job should be
	// cancelled.
	require.NoError(engine.Put(context.Background(), request.NodeID, request.RequestID, nil))
	require.Empty(engine.blocked)
}

func TestEngineQuery(t *testing.T) {
	require := require.New(t)

	peerID, _, sender, vm, engine := setup(t)

	blks := BuildChain(Genesis, 2)
	parent := blks[0]
	child := blks[1]

	var sendChitsCalled bool
	sender.SendChitsF = func(_ context.Context, _ ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDByHeight ids.ID, accepted ids.ID) {
		require.False(sendChitsCalled)
		sendChitsCalled = true
		require.Equal(uint32(15), requestID)
		require.Equal(GenesisID, preferredID)
		require.Equal(GenesisID, preferredIDByHeight)
		require.Equal(GenesisID, accepted)
	}

	var getBlockCalled bool
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		getBlockCalled = true

		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var getRequest *common.Request
	sender.SendGetF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
		require.Nil(getRequest)
		getRequest = &common.Request{
			NodeID:    nodeID,
			RequestID: requestID,
		}
		require.Equal(peerID, nodeID)
		require.Contains([]ids.ID{
			parent.ID(),
			GenesisID,
		}, blkID)
	}

	// Handling a pull query for [parent] should result in immediately
	// responding with chits for [Genesis] along with a request for [parent].
	require.NoError(engine.PullQuery(context.Background(), peerID, 15, parent.ID(), 1))
	require.True(sendChitsCalled)
	require.True(getBlockCalled)
	require.NotNil(getRequest)

	var queryRequest *common.Request
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blockID ids.ID, requestedHeight uint64) {
		require.Nil(queryRequest)
		require.Equal(set.Of(peerID), nodeIDs)
		queryRequest = &common.Request{
			NodeID:    peerID,
			RequestID: requestID,
		}
		require.Equal(parent.ID(), blockID)
		require.Equal(uint64(1), requestedHeight)
	}

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(parent.Bytes(), b)
		return parent, nil
	}

	// After receiving [parent], the engine will parse it, issue it, and then
	// send a pull query.
	require.NoError(engine.Put(context.Background(), getRequest.NodeID, getRequest.RequestID, parent.Bytes()))
	require.NotNil(queryRequest)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case parent.ID(), child.ID():
			return nil, errUnknownBlock
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}
	vm.ParseBlockF = nil

	getRequest = nil
	sender.SendGetF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
		require.Nil(getRequest)
		getRequest = &common.Request{
			NodeID:    nodeID,
			RequestID: requestID,
		}
		require.Equal(peerID, nodeID)
		require.Equal(child.ID(), blkID)
	}

	// Handling chits for [child] register a voter job blocking on [child]'s
	// issuance and send a request for [child].
	require.NoError(engine.Chits(context.Background(), queryRequest.NodeID, queryRequest.RequestID, child.ID(), child.ID(), child.ID()))

	queryRequest = nil
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blockID ids.ID, requestedHeight uint64) {
		require.Nil(queryRequest)
		require.Equal(set.Of(peerID), nodeIDs)
		queryRequest = &common.Request{
			NodeID:    peerID,
			RequestID: requestID,
		}
		require.Equal(child.ID(), blockID)
		require.Equal(uint64(1), requestedHeight)
	}

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(child.Bytes(), b)

		vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			switch blkID {
			case parent.ID():
				return parent, nil
			case child.ID():
				return child, nil
			}
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}

		return child, nil
	}

	// After receiving [child], the engine will parse it, issue it, and then
	// apply the votes received during the poll for [parent]. Applying the votes
	// should cause both [parent] and [child] to be accepted.
	require.NoError(engine.Put(context.Background(), getRequest.NodeID, getRequest.RequestID, child.Bytes()))
	require.Equal(choices.Accepted, parent.Status())
	require.Equal(choices.Accepted, child.Status())
	require.Empty(engine.blocked)
}

func TestEngineMultipleQuery(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params = snowball.Parameters{
		K:                     3,
		AlphaPreference:       2,
		AlphaConfidence:       2,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	vals := validators.NewManager()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()
	vdr2 := ids.GenerateTestNodeID()

	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr0, nil, ids.Empty, 1))
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr1, nil, ids.Empty, 1))
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr2, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, blkID)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 2)
	blk0 := blks[0]
	blk1 := blks[1]

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		*queryRequestID = requestID
		vdrSet := set.Of(vdr0, vdr1, vdr2)
		require.Equal(vdrSet, inVdrs)
		require.Equal(blk0.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk0,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case GenesisID:
			return Genesis, nil
		case blk0.ID():
			return blk0, nil
		case blk1.ID():
			return nil, errUnknownBlock
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	asked := new(bool)
	getRequestID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		require.False(*asked)
		*asked = true
		*getRequestID = requestID
		require.Equal(vdr0, inVdr)
		require.Equal(blk1.ID(), blkID)
	}
	require.NoError(te.Chits(context.Background(), vdr0, *queryRequestID, blk1.ID(), blk1.ID(), blk1.ID()))
	require.NoError(te.Chits(context.Background(), vdr1, *queryRequestID, blk1.ID(), blk1.ID(), blk1.ID()))

	vm.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			switch {
			case blkID == blk0.ID():
				return blk0, nil
			case blkID == blk1.ID():
				return blk1, nil
			}
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}

		return blk1, nil
	}

	*queried = false
	secondQueryRequestID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		*secondQueryRequestID = requestID
		vdrSet := set.Of(vdr0, vdr1, vdr2)
		require.Equal(vdrSet, inVdrs)
		require.Equal(blk1.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}
	require.NoError(te.Put(context.Background(), vdr0, *getRequestID, blk1.Bytes()))

	// Should be dropped because the query was already filled
	require.NoError(te.Chits(context.Background(), vdr2, *queryRequestID, blk0.ID(), blk0.ID(), blk0.ID()))

	require.Equal(choices.Accepted, blk1.Status())
	require.Empty(te.blocked)
}

func TestEngineBlockedIssue(t *testing.T) {
	require := require.New(t)

	_, _, sender, vm, te := setup(t)

	sender.Default(false)

	blks := BuildChain(Genesis, 2)
	blk0 := blks[0]
	blk1 := blks[1]

	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk0.ID():
			return blk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk1,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk0,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.Equal(blk1.ID(), te.Consensus.Preference())
}

func TestEngineRespondsToGetRequest(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(false)

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, id)
		return Genesis, nil
	}

	var sentPut bool
	sender.SendPutF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, blk []byte) {
		require.False(sentPut)
		sentPut = true

		require.Equal(vdr, nodeID)
		require.Equal(uint32(123), requestID)
		require.Equal(GenesisBytes, blk)
	}

	require.NoError(te.Get(context.Background(), vdr, 123, GenesisID))
	require.True(sentPut)
}

func TestEnginePushQuery(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, blk.Bytes()) {
			return blk, nil
		}
		return nil, errUnknownBytes
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk.ID():
			return blk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	chitted := new(bool)
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDByHeight ids.ID, acceptedID ids.ID) {
		require.False(*chitted)
		*chitted = true
		require.Equal(vdr, inVdr)
		require.Equal(uint32(20), requestID)
		require.Equal(GenesisID, preferredID)
		require.Equal(GenesisID, preferredIDByHeight)
		require.Equal(GenesisID, acceptedID)
	}

	queried := new(bool)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], _ uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		vdrSet := set.Of(vdr)
		require.True(inVdrs.Equals(vdrSet))
		require.Equal(blk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	require.NoError(te.PushQuery(context.Background(), vdr, 20, blk.Bytes(), 1))

	require.True(*chitted)
	require.True(*queried)
}

func TestEngineBuildBlock(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID, uint64) {
		require.FailNow("should not be sending pulls when we are the block producer")
	}

	pushSent := new(bool)
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], _ uint32, _ []byte, _ uint64) {
		require.False(*pushSent)
		*pushSent = true
		vdrSet := set.Of(vdr)
		require.Equal(vdrSet, inVdrs)
	}

	vm.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return blk, nil
	}
	require.NoError(te.Notify(context.Background(), common.PendingTxs))

	require.True(*pushSent)
}

func TestEngineRepoll(t *testing.T) {
	require := require.New(t)
	vdr, _, sender, _, te := setup(t)

	sender.Default(true)

	queried := new(bool)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], _ uint32, _ ids.ID, _ uint64) {
		require.False(*queried)
		*queried = true
		vdrSet := set.Of(vdr)
		require.Equal(vdrSet, inVdrs)
	}

	te.repoll(context.Background())

	require.True(*queried)
}

func TestVoteCanceling(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params = snowball.Parameters{
		K:                     3,
		AlphaPreference:       2,
		AlphaConfidence:       2,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	vals := validators.NewManager()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()
	vdr2 := ids.GenerateTestNodeID()

	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr0, nil, ids.Empty, 1))
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr1, nil, ids.Empty, 1))
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr2, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, id)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkBytes []byte, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		*queryRequestID = requestID
		vdrSet := set.Of(vdr0, vdr1, vdr2)
		require.Equal(vdrSet, inVdrs)
		require.Equal(blk.Bytes(), blkBytes)
		require.Equal(uint64(1), requestedHeight)
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		true,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.Equal(1, te.polls.Len())

	require.NoError(te.QueryFailed(context.Background(), vdr0, *queryRequestID))

	require.Equal(1, te.polls.Len())

	repolled := new(bool)
	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID, uint64) {
		*repolled = true
	}
	require.NoError(te.QueryFailed(context.Background(), vdr1, *queryRequestID))

	require.True(*repolled)
}

func TestEngineNoQuery(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == GenesisID {
			return Genesis, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
}

func TestEngineNoRepollQuery(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == GenesisID {
			return Genesis, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	te.repoll(context.Background())
}

func TestEngineAbandonQuery(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blkID := ids.GenerateTestID()

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(blkID, id)
		return nil, errUnknownBlock
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		*reqID = requestID
	}

	sender.CantSendChits = false

	require.NoError(te.PullQuery(context.Background(), vdr, 0, blkID, 0))

	require.Equal(1, te.blkReqs.Len())

	require.NoError(te.GetFailed(context.Background(), vdr, *reqID))

	require.Zero(te.blkReqs.Len())
}

func TestEngineAbandonChit(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk.ID():
			return nil, errUnknownBlock
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	var reqID uint32
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		reqID = requestID
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	fakeBlkID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(fakeBlkID, id)
		return nil, errUnknownBlock
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		reqID = requestID
	}

	// Register a voter dependency on an unknown block.
	require.NoError(te.Chits(context.Background(), vdr, reqID, fakeBlkID, fakeBlkID, fakeBlkID))
	require.Len(te.blocked, 1)

	sender.CantSendPullQuery = false

	require.NoError(te.GetFailed(context.Background(), vdr, reqID))
	require.Empty(te.blocked)
}

func TestEngineAbandonChitWithUnexpectedPutBlock(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk.ID():
			return nil, errUnknownBlock
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte, _ uint64) {
		reqID = requestID
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		true,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	fakeBlkID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(fakeBlkID, id)
		return nil, errUnknownBlock
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		reqID = requestID
	}

	// Register a voter dependency on an unknown block.
	require.NoError(te.Chits(context.Background(), vdr, reqID, fakeBlkID, fakeBlkID, fakeBlkID))
	require.Len(te.blocked, 1)

	sender.CantSendPullQuery = false

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(GenesisBytes, b)
		return Genesis, nil
	}

	// Respond with an unexpected block and verify that the request is correctly
	// cleared.
	require.NoError(te.Put(context.Background(), vdr, reqID, GenesisBytes))
	require.Empty(te.blocked)
}

func TestEngineBlockingChitRequest(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 3)
	missingBlk := blks[0]
	parentBlk := blks[1]
	blockingBlk := blks[2]

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blockingBlk.ID():
			return blockingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {}

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(blockingBlk.Bytes(), b)
		return blockingBlk, nil
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		parentBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	sender.CantSendChits = false

	require.NoError(te.PushQuery(context.Background(), vdr, 0, blockingBlk.Bytes(), 0))

	require.Len(te.blocked, 2)

	sender.CantSendPullQuery = false

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		missingBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.Empty(te.blocked)
}

func TestEngineBlockingChitResponse(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	issuedBlks := BuildChain(Genesis, 1)
	issuedBlk := issuedBlks[0]

	blockingBlks := BuildChain(Genesis, 2)
	missingBlk := blockingBlks[0]
	blockingBlk := blockingBlks[1]

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case issuedBlk.ID():
			return issuedBlk, nil
		case blockingBlk.ID():
			return blockingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blockingBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	queryRequestID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		*queryRequestID = requestID
		vdrSet := set.Of(vdr)
		require.Equal(vdrSet, inVdrs)
		require.Equal(issuedBlk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		issuedBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	sender.SendPushQueryF = nil
	sender.CantSendPushQuery = false

	require.NoError(te.Chits(context.Background(), vdr, *queryRequestID, blockingBlk.ID(), issuedBlk.ID(), blockingBlk.ID()))

	require.Len(te.blocked, 2)
	sender.CantSendPullQuery = false

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		missingBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
}

func TestEngineRetryFetch(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 1)
	missingBlk := blks[0]

	vm.CantGetBlock = false

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		*reqID = requestID
	}
	sender.CantSendChits = false

	require.NoError(te.PullQuery(context.Background(), vdr, 0, missingBlk.ID(), 0))

	vm.CantGetBlock = true
	sender.SendGetF = nil

	require.NoError(te.GetFailed(context.Background(), vdr, *reqID))

	vm.CantGetBlock = false

	called := new(bool)
	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {
		*called = true
	}

	require.NoError(te.PullQuery(context.Background(), vdr, 0, missingBlk.ID(), 0))

	vm.CantGetBlock = true
	sender.SendGetF = nil

	require.True(*called)
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 2)
	validBlk := blks[0]

	invalidBlk := blks[1]
	invalidBlk.VerifyV = errTest

	invalidBlkID := invalidBlk.ID()

	reqID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		*reqID = requestID
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case validBlk.ID():
			return validBlk, nil
		case invalidBlk.ID():
			return invalidBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		validBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
	sender.SendPushQueryF = nil
	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		invalidBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
	require.NoError(te.Chits(context.Background(), vdr, *reqID, invalidBlkID, invalidBlkID, invalidBlkID))

	require.Equal(choices.Accepted, validBlk.Status())
}

func TestEngineGossip(t *testing.T) {
	require := require.New(t)

	nodeID, _, sender, vm, te := setup(t)

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, blkID)
		return Genesis, nil
	}

	var calledSendPullQuery bool
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ ids.ID, _ uint64) {
		calledSendPullQuery = true
		require.Equal(set.Of(nodeID), nodeIDs)
	}

	require.NoError(te.Gossip(context.Background()))

	require.True(calledSendPullQuery)
}

func TestEngineInvalidBlockIgnoredFromUnexpectedPeer(t *testing.T) {
	require := require.New(t)

	vdr, vdrs, sender, vm, te := setup(t)

	secondVdr := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(te.Ctx.SubnetID, secondVdr, nil, ids.Empty, 1))

	sender.Default(true)

	blks := BuildChain(Genesis, 2)
	missingBlk := blks[0]
	pendingBlk := blks[1]

	parsed := new(bool)
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, pendingBlk.Bytes()) {
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case pendingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return pendingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, reqVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		require.Equal(vdr, reqVdr)
		require.Equal(missingBlk.ID(), blkID)
	}
	sender.CantSendChits = false

	require.NoError(te.PushQuery(context.Background(), vdr, 0, pendingBlk.Bytes(), 0))

	require.NoError(te.Put(context.Background(), secondVdr, *reqID, []byte{3}))

	*parsed = false
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, missingBlk.Bytes()) {
			*parsed = true
			return missingBlk, nil
		}
		return nil, errUnknownBlock
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case missingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return missingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	sender.CantSendPullQuery = false

	require.NoError(te.Put(context.Background(), vdr, *reqID, missingBlk.Bytes()))

	require.Equal(pendingBlk.ID(), te.Consensus.Preference())
}

func TestEnginePushQueryRequestIDConflict(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 2)
	missingBlk := blks[0]
	pendingBlk := blks[1]

	parsed := new(bool)
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, pendingBlk.Bytes()) {
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case pendingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return pendingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, reqVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		require.Equal(vdr, reqVdr)
		require.Equal(missingBlk.ID(), blkID)
	}
	sender.CantSendChits = false

	require.NoError(te.PushQuery(context.Background(), vdr, 0, pendingBlk.Bytes(), 0))

	sender.SendGetF = nil
	sender.CantSendGet = false

	require.NoError(te.PushQuery(context.Background(), vdr, *reqID, []byte{3}, 0))

	*parsed = false
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, missingBlk.Bytes()) {
			*parsed = true
			return missingBlk, nil
		}
		return nil, errUnknownBlock
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case missingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return missingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	sender.CantSendPullQuery = false

	require.NoError(te.Put(context.Background(), vdr, *reqID, missingBlk.Bytes()))

	require.Equal(pendingBlk.ID(), te.Consensus.Preference())
}

func TestEngineAggressivePolling(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params.ConcurrentRepolls = 2

	vals := validators.NewManager()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, blkID)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 1)
	pendingBlk := blks[0]

	parsed := new(bool)
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, pendingBlk.Bytes()) {
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case pendingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return pendingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	numPulled := new(int)
	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID, uint64) {
		*numPulled++
	}

	require.NoError(te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()))

	require.Equal(2, *numPulled)
}

func TestEngineDoubleChit(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params = snowball.Parameters{
		K:                     2,
		AlphaPreference:       2,
		AlphaConfidence:       2,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	vals := validators.NewManager()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()

	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr0, nil, ids.Empty, 1))
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr1, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, id)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.False((*queried))
		*queried = true
		*queryRequestID = requestID
		vdrSet := set.Of(vdr0, vdr1)
		require.Equal(vdrSet, inVdrs)
		require.Equal(blk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}
	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case GenesisID:
			return Genesis, nil
		case blk.ID():
			return blk, nil
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	require.Equal(choices.Processing, blk.Status())

	require.NoError(te.Chits(context.Background(), vdr0, *queryRequestID, blk.ID(), blk.ID(), blk.ID()))
	require.Equal(choices.Processing, blk.Status())

	require.NoError(te.Chits(context.Background(), vdr0, *queryRequestID, blk.ID(), blk.ID(), blk.ID()))
	require.Equal(choices.Processing, blk.Status())

	require.NoError(te.Chits(context.Background(), vdr1, *queryRequestID, blk.ID(), blk.ID(), blk.ID()))
	require.Equal(choices.Accepted, blk.Status())
}

func TestEngineBuildBlockLimit(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params.K = 1
	engCfg.Params.AlphaPreference = 1
	engCfg.Params.AlphaConfidence = 1
	engCfg.Params.OptimalProcessing = 1

	vals := validators.NewManager()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, blkID)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 2)
	blk0 := blks[0]

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], rID uint32, _ []byte, _ uint64) {
		reqID = rID
		require.False(queried)
		queried = true
		vdrSet := set.Of(vdr)
		require.Equal(vdrSet, inVdrs)
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	blkToReturn := 0
	vm.BuildBlockF = func(context.Context) (snowman.Block, error) {
		require.Less(blkToReturn, len(blks))
		blk := blks[blkToReturn]
		blkToReturn++
		return blk, nil
	}
	require.NoError(te.Notify(context.Background(), common.PendingTxs))

	require.True(queried)

	queried = false
	require.NoError(te.Notify(context.Background(), common.PendingTxs))

	require.False(queried)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk0.ID():
			return blk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(te.Chits(context.Background(), vdr, reqID, blk0.ID(), blk0.ID(), blk0.ID()))

	require.True(queried)
}

func TestEngineReceiveNewRejectedBlock(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	acceptedBlks := BuildChain(Genesis, 1)
	acceptedBlk := acceptedBlks[0]

	rejectedBlks := BuildChain(Genesis, 2)
	rejectedBlk := rejectedBlks[0]
	pendingBlk := rejectedBlks[1]

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		asked bool
		reqID uint32
	)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], rID uint32, _ ids.ID, _ uint64) {
		asked = true
		reqID = rID
	}

	require.NoError(te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()))

	require.True(asked)

	require.NoError(te.Chits(context.Background(), vdr, reqID, acceptedBlk.ID(), acceptedBlk.ID(), acceptedBlk.ID()))

	sender.SendPullQueryF = nil
	asked = false

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, rID uint32, _ ids.ID) {
		asked = true
		reqID = rID
	}

	require.NoError(te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()))

	require.True(asked)

	rejectedBlk.StatusV = choices.Rejected

	require.NoError(te.Put(context.Background(), vdr, reqID, rejectedBlk.Bytes()))

	require.Zero(te.blkReqs.Len())
}

func TestEngineRejectionAmplification(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	acceptedBlks := BuildChain(Genesis, 1)
	acceptedBlk := acceptedBlks[0]

	rejectedBlks := BuildChain(Genesis, 2)
	rejectedBlk := rejectedBlks[0]
	pendingBlk := rejectedBlks[1]

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], rID uint32, _ ids.ID, _ uint64) {
		queried = true
		reqID = rID
	}

	require.NoError(te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()))

	require.True(queried)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(te.Chits(context.Background(), vdr, reqID, acceptedBlk.ID(), acceptedBlk.ID(), acceptedBlk.ID()))

	require.Zero(te.Consensus.NumProcessing())

	queried = false
	var asked bool
	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID, uint64) {
		queried = true
	}
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, rID uint32, blkID ids.ID) {
		asked = true
		reqID = rID

		require.Equal(rejectedBlk.ID(), blkID)
	}

	require.NoError(te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()))

	require.False(queried)
	require.True(asked)

	require.NoError(te.Put(context.Background(), vdr, reqID, rejectedBlk.Bytes()))

	require.False(queried)
}

// Test that the node will not issue a block into consensus that it knows will
// be rejected because the parent is rejected.
func TestEngineTransitiveRejectionAmplificationDueToRejectedParent(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	acceptedBlks := BuildChain(Genesis, 1)
	acceptedBlk := acceptedBlks[0]

	rejectedBlks := BuildChain(Genesis, 2)
	rejectedBlk := rejectedBlks[0]
	pendingBlk := rejectedBlks[1]
	pendingBlk.RejectV = errUnexpectedCall

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		case rejectedBlk.ID():
			return rejectedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], rID uint32, _ ids.ID, _ uint64) {
		queried = true
		reqID = rID
	}

	require.NoError(te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()))

	require.True(queried)

	require.NoError(te.Chits(context.Background(), vdr, reqID, acceptedBlk.ID(), acceptedBlk.ID(), acceptedBlk.ID()))

	require.Zero(te.Consensus.NumProcessing())

	require.NoError(te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()))

	require.Zero(te.Consensus.NumProcessing())

	require.Empty(te.pending)
}

// Test that the node will not issue a block into consensus that it knows will
// be rejected because the parent is failing verification.
func TestEngineTransitiveRejectionAmplificationDueToInvalidParent(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	acceptedBlks := BuildChain(Genesis, 1)
	acceptedBlk := acceptedBlks[0]

	rejectedBlks := BuildChain(Genesis, 2)
	rejectedBlk := rejectedBlks[0]
	rejectedBlk.VerifyV = errUnexpectedCall
	pendingBlk := rejectedBlks[1]
	pendingBlk.RejectV = errUnexpectedCall

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], rID uint32, _ ids.ID, _ uint64) {
		queried = true
		reqID = rID
	}

	require.NoError(te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()))
	require.True(queried)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case rejectedBlk.ID():
			return rejectedBlk, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(te.Chits(context.Background(), vdr, reqID, acceptedBlk.ID(), acceptedBlk.ID(), acceptedBlk.ID()))

	require.NoError(te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()))
	require.Zero(te.Consensus.NumProcessing())
	require.Empty(te.pending)
}

// Test that the node will not gossip a block that isn't preferred.
func TestEngineNonPreferredAmplification(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)

	preferredBlks := BuildChain(Genesis, 1)
	preferredBlk := preferredBlks[0]

	nonPreferredBlks := BuildChain(Genesis, 1)
	nonPreferredBlk := nonPreferredBlks[0]

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, preferredBlk.Bytes()):
			return preferredBlk, nil
		case bytes.Equal(b, nonPreferredBlk.Bytes()):
			return nonPreferredBlk, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, blkBytes []byte, requestedHeight uint64) {
		require.NotEqual(nonPreferredBlk.Bytes(), blkBytes)
		require.Equal(uint64(1), requestedHeight)
	}
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, blkID ids.ID, requestedHeight uint64) {
		require.NotEqual(nonPreferredBlk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	require.NoError(te.Put(context.Background(), vdr, 0, preferredBlk.Bytes()))

	require.NoError(te.Put(context.Background(), vdr, 0, nonPreferredBlk.Bytes()))
}

// Test that in the following scenario, if block B fails verification, votes
// will still be bubbled through to the valid block A. This is a regression test
// to ensure that the consensus engine correctly handles the case that votes can
// be bubbled correctly through a block that cannot pass verification until one
// of its ancestors has been marked as accepted.
//
//	G
//	|
//	A
//	|
//	B
func TestEngineBubbleVotesThroughInvalidBlock(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)
	expectedVdrSet := set.Of(vdr)

	blks := BuildChain(Genesis, 2)
	blk1 := blks[0]

	// blk2 cannot pass verification until [blk1] has been marked as accepted.
	blk2 := blks[1]
	blk2.VerifyV = errInvalid

	// The VM should be able to parse [blk1] and [blk2]
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, blk1.Bytes()):
			return blk1, nil
		case bytes.Equal(b, blk2.Bytes()):
			return blk2, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	// for now, this VM should only be able to retrieve [Genesis] from storage
	// this "GetBlockF" will be updated after blocks are verified/accepted
	// in the following tests
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		require.False(*asked)
		require.Equal(blk1.ID(), blkID)
		require.Equal(vdr, inVdr)
		*asked = true
	}
	sender.CantSendChits = false

	// This engine receives a Gossip message for [blk2] which was "unknown" in this engine.
	// The engine thus learns about its ancestor [blk1] and should send a Get request for it.
	// (see above for expected "Get" request)
	require.NoError(te.PushQuery(context.Background(), vdr, 0, blk2.Bytes(), 0))
	require.True(*asked)

	// Prepare to PullQuery [blk1] after our Get request is fulfilled. We should not PullQuery
	// [blk2] since it currently fails verification.
	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true

		*queryRequestID = requestID
		vdrSet := set.Of(vdr)
		require.Equal(vdrSet, inVdrs)
		require.Equal(blk1.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}
	// This engine now handles the response to the "Get" request. This should cause [blk1] to be issued
	// which will result in attempting to issue [blk2]. However, [blk2] should fail verification and be dropped.
	// By issuing [blk1], this node should fire a "PullQuery" request for [blk1].
	// (see above for expected "PullQuery" request)
	require.NoError(te.Put(context.Background(), vdr, *reqID, blk1.Bytes()))
	require.True(*asked)
	require.True(*queried, "Didn't query the newly issued blk1")

	// now [blk1] is verified, vm can return it
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk1.ID():
			return blk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sendReqID := new(uint32)
	reqVdr := new(ids.NodeID)
	// Update GetF to produce a more detailed error message in the case that receiving a Chits
	// message causes us to send another Get request.
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		require.Equal(blk2.ID(), blkID)

		*sendReqID = requestID
		*reqVdr = inVdr
	}

	// Now we are expecting a Chits message, and we receive it for [blk2]
	// instead of [blk1]. This will cause the node to again request [blk2].
	require.NoError(te.Chits(context.Background(), vdr, *queryRequestID, blk2.ID(), blk1.ID(), blk2.ID()))

	// The votes should be bubbled through [blk2] despite the fact that it is
	// failing verification.
	require.NoError(te.Put(context.Background(), *reqVdr, *sendReqID, blk2.Bytes()))

	// The vote should be bubbled through [blk2], such that [blk1] gets marked as Accepted.
	require.Equal(choices.Accepted, blk1.Status())
	require.Equal(choices.Processing, blk2.Status())

	// Now that [blk1] has been marked as Accepted, [blk2] can pass verification.
	blk2.VerifyV = nil
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk1.ID():
			return blk1, nil
		case blk2.ID():
			return blk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	*queried = false
	// Prepare to PullQuery [blk2] after receiving a Gossip message with [blk2].
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		*queryRequestID = requestID
		require.Equal(expectedVdrSet, inVdrs)
		require.Equal(blk2.ID(), blkID)
		require.Equal(uint64(2), requestedHeight)
	}
	// Expect that the Engine will send a PullQuery after receiving this Gossip message for [blk2].
	require.NoError(te.PushQuery(context.Background(), vdr, 0, blk2.Bytes(), 0))
	require.True(*queried)

	// After a single vote for [blk2], it should be marked as accepted.
	require.NoError(te.Chits(context.Background(), vdr, *queryRequestID, blk2.ID(), blk1.ID(), blk2.ID()))
	require.Equal(choices.Accepted, blk2.Status())
}

// Test that in the following scenario, if block B fails verification, votes
// will still be bubbled through from block C to the valid block A. This is a
// regression test to ensure that the consensus engine correctly handles the
// case that votes can be bubbled correctly through a chain that cannot pass
// verification until one of its ancestors has been marked as accepted.
//
//	G
//	|
//	A
//	|
//	B
//	|
//	C
func TestEngineBubbleVotesThroughInvalidChain(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t)
	expectedVdrSet := set.Of(vdr)

	blks := BuildChain(Genesis, 3)
	blk1 := blks[0]

	// blk2 cannot pass verification until [blk1] has been marked as accepted.
	blk2 := blks[1]
	blk2.VerifyV = errInvalid

	blk3 := blks[2]

	// The VM should be able to parse [blk1], [blk2], and [blk3]
	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, blk1.Bytes()):
			return blk1, nil
		case bytes.Equal(b, blk2.Bytes()):
			return blk2, nil
		case bytes.Equal(b, blk3.Bytes()):
			return blk3, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	// The VM should be able to retrieve [Genesis] and [blk1] from storage
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case blk1.ID():
			return blk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		require.False(*asked)
		require.Equal(blk2.ID(), blkID)
		require.Equal(vdr, inVdr)
		*asked = true
	}
	sender.CantSendChits = false

	// Receive Gossip message for [blk3] first and expect the sender to issue a
	// Get request for its ancestor: [blk2].
	require.NoError(te.PushQuery(context.Background(), vdr, 0, blk3.Bytes(), 0))
	require.True(*asked)

	// Prepare to PullQuery [blk1] after our request for [blk2] is fulfilled.
	// We should not PullQuery [blk2] since it currently fails verification.
	// We should not PullQuery [blk3] because [blk2] wasn't issued.
	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		*queryRequestID = requestID
		require.Equal(expectedVdrSet, inVdrs)
		require.Equal(blk1.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	// Answer the request, this should result in [blk1] being issued as well.
	require.NoError(te.Put(context.Background(), vdr, *reqID, blk2.Bytes()))
	require.True(*queried)

	sendReqID := new(uint32)
	reqVdr := new(ids.NodeID)
	// Update GetF to produce a more detailed error message in the case that receiving a Chits
	// message causes us to send another Get request.
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		switch blkID {
		case blk1.ID():
			require.FailNow("Unexpectedly sent a Get request for blk1")
		case blk2.ID():
			t.Logf("sending get for blk2 with %d", requestID)
			*sendReqID = requestID
			*reqVdr = inVdr
			return
		case blk3.ID():
			t.Logf("sending get for blk3 with %d", requestID)
			*sendReqID = requestID
			*reqVdr = inVdr
			return
		default:
			require.FailNow("Unexpectedly sent a Get request for unknown block")
		}
	}

	// Now we are expecting a Chits message and we receive it for [blk3].
	// This will cause the node to again request [blk3].
	require.NoError(te.Chits(context.Background(), vdr, *queryRequestID, blk3.ID(), blk1.ID(), blk3.ID()))

	// Drop the re-request for [blk3] to cause the poll to terminate. The votes
	// should be bubbled through [blk3] despite the fact that it hasn't been
	// issued.
	require.NoError(te.GetFailed(context.Background(), *reqVdr, *sendReqID))

	// The vote should be bubbled through [blk3] and [blk2] such that [blk1]
	// gets marked as Accepted.
	require.Equal(choices.Accepted, blk1.Status())
}

func TestEngineBuildBlockWithCachedNonVerifiedParent(t *testing.T) {
	require := require.New(t)
	vdr, _, sender, vm, te := setup(t)

	sender.Default(true)

	blks := BuildChain(Genesis, 3)
	grandParentBlk := blks[0]

	parentBlkA := blks[1]
	parentBlkA.VerifyV = errInvalid

	// Note that [parentBlkB] has the same [ID()] as [parentBlkA];
	// it's a different instantiation of the same block.
	parentBlkB := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     parentBlkA.IDV,
			StatusV: parentBlkA.StatusV,
		},
		ParentV: parentBlkA.ParentV,
		HeightV: parentBlkA.HeightV,
		BytesV:  parentBlkA.BytesV,
	}

	// Child of [parentBlkA]/[parentBlkB]
	childBlk := blks[2]

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(grandParentBlk.BytesV, b)
		return grandParentBlk, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case grandParentBlk.IDV:
			return grandParentBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	queryRequestGPID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		*queryRequestGPID = requestID
		require.Equal(grandParentBlk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	// Give the engine the grandparent
	require.NoError(te.Put(context.Background(), vdr, 0, grandParentBlk.BytesV))

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(parentBlkA.BytesV, b)
		return parentBlkA, nil
	}

	// Give the node [parentBlkA]/[parentBlkB].
	// When it's parsed we get [parentBlkA] (not [parentBlkB]).
	// [parentBlkA] fails verification and gets put into [te.nonVerifiedCache].
	require.NoError(te.Put(context.Background(), vdr, 0, parentBlkA.BytesV))

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(parentBlkB.BytesV, b)
		return parentBlkB, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case GenesisID:
			return Genesis, nil
		case grandParentBlk.IDV:
			return grandParentBlk, nil
		case parentBlkB.IDV:
			return parentBlkB, nil
		default:
			return nil, errUnknownBlock
		}
	}

	queryRequestAID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		*queryRequestAID = requestID
		require.Equal(parentBlkA.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}
	sender.CantSendPullQuery = false

	// Give the engine [parentBlkA]/[parentBlkB] again.
	// This time when we parse it we get [parentBlkB] (not [parentBlkA]).
	// When we fetch it using [GetBlockF] we get [parentBlkB].
	// Note that [parentBlkB] doesn't fail verification and is issued into consensus.
	// This evicts [parentBlkA] from [te.nonVerifiedCache].
	require.NoError(te.Put(context.Background(), vdr, 0, parentBlkA.BytesV))

	// Give 2 chits for [parentBlkA]/[parentBlkB]
	require.NoError(te.Chits(context.Background(), vdr, *queryRequestAID, parentBlkB.IDV, grandParentBlk.IDV, parentBlkB.IDV))
	require.NoError(te.Chits(context.Background(), vdr, *queryRequestGPID, parentBlkB.IDV, grandParentBlk.IDV, parentBlkB.IDV))

	// Assert that the blocks' statuses are correct.
	// The evicted [parentBlkA] shouldn't be changed.
	require.Equal(choices.Processing, parentBlkA.Status())
	require.Equal(choices.Accepted, parentBlkB.Status())

	vm.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return childBlk, nil
	}

	sentQuery := new(bool)
	sender.SendPushQueryF = func(context.Context, set.Set[ids.NodeID], uint32, []byte, uint64) {
		*sentQuery = true
	}

	// Should issue a new block and send a query for it.
	require.NoError(te.Notify(context.Background(), common.PendingTxs))
	require.True(*sentQuery)
}

func TestEngineApplyAcceptedFrontierInQueryFailed(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params = snowball.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		BetaVirtuous:          2,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	vals := validators.NewManager()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, id)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)
	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkBytes []byte, requestedHeight uint64) {
		*queryRequestID = requestID
		require.Contains(inVdrs, vdr)
		require.Equal(blk.Bytes(), blkBytes)
		require.Equal(uint64(1), requestedHeight)
	}

	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		true,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case GenesisID:
			return Genesis, nil
		case blk.ID():
			return blk, nil
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	require.Equal(choices.Processing, blk.Status())

	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		*queryRequestID = requestID
		require.Contains(inVdrs, vdr)
		require.Equal(blk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	require.NoError(te.Chits(context.Background(), vdr, *queryRequestID, blk.ID(), blk.ID(), blk.ID()))

	require.Equal(choices.Processing, blk.Status())

	require.NoError(te.QueryFailed(context.Background(), vdr, *queryRequestID))

	require.Equal(choices.Accepted, blk.Status())
}

func TestEngineRepollsMisconfiguredSubnet(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)
	engCfg.Params = snowball.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	// Setup the engine with no validators. When a block is issued, the poll
	// should fail to be created because there is nobody to poll.
	vals := validators.NewManager()
	engCfg.Validators = vals

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(GenesisID, id)
		return Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)
	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blks := BuildChain(Genesis, 1)
	blk := blks[0]

	// Issue the block. This shouldn't call the sender, because creating the
	// poll should fail.
	require.NoError(te.issue(
		context.Background(),
		te.Ctx.NodeID,
		blk,
		true,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	// The block should have successfully been added into consensus.
	require.Equal(1, te.Consensus.NumProcessing())

	// Fix the subnet configuration by adding a validator.
	vdr := ids.GenerateTestNodeID()
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr, nil, ids.Empty, 1))

	var (
		queryRequestID uint32
		queried        bool
	)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		queryRequestID = requestID
		require.Contains(inVdrs, vdr)
		require.Equal(blk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
		queried = true
	}

	// Because there is now a validator that can be queried, gossip should
	// trigger creation of the poll.
	require.NoError(te.Gossip(context.Background()))
	require.True(queried)

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case GenesisID:
			return Genesis, nil
		case blk.ID():
			return blk, nil
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	// Voting for the block that was issued during the period when the validator
	// set was misconfigured should result in it being accepted successfully.
	require.NoError(te.Chits(context.Background(), vdr, queryRequestID, blk.ID(), blk.ID(), blk.ID()))
	require.Equal(choices.Accepted, blk.Status())
}
