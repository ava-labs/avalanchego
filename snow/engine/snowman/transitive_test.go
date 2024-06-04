// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/ancestor"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errUnknownBlock   = errors.New("unknown block")
	errUnknownBytes   = errors.New("unknown bytes")
	errInvalid        = errors.New("invalid")
	errUnexpectedCall = errors.New("unexpected call")
	errTest           = errors.New("non-nil test")
)

func MakeGetBlockF(blks ...[]*snowmantest.Block) func(context.Context, ids.ID) (snowman.Block, error) {
	return func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		for _, blkSet := range blks {
			for _, blk := range blkSet {
				if blkID == blk.ID() {
					return blk, nil
				}
			}
		}
		return nil, errUnknownBlock
	}
}

func MakeParseBlockF(blks ...[]*snowmantest.Block) func(context.Context, []byte) (snowman.Block, error) {
	return func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blkSet := range blks {
			for _, blk := range blkSet {
				if bytes.Equal(blkBytes, blk.Bytes()) {
					return blk, nil
				}
			}
		}
		return nil, errUnknownBlock
	}
}

func MakeLastAcceptedBlockF(defaultBlk *snowmantest.Block, blks ...[]*snowmantest.Block) func(context.Context) (ids.ID, error) {
	return func(_ context.Context) (ids.ID, error) {
		highestHeight := defaultBlk.Height()
		highestID := defaultBlk.ID()
		for _, blkSet := range blks {
			for _, blk := range blkSet {
				if blk.Status() == choices.Accepted && blk.Height() > highestHeight {
					highestHeight = blk.Height()
					highestID = blk.ID()
				}
			}
		}
		return highestID, nil
	}
}

func setup(t *testing.T, config Config) (ids.NodeID, validators.Manager, *common.SenderTest, *block.TestVM, *Transitive) {
	require := require.New(t)

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
		return snowmantest.GenesisID, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	peerID, _, sender, vm, engine := setup(t, DefaultConfig(t))

	parent := snowmantest.BuildChild(snowmantest.Genesis)
	child := snowmantest.BuildChild(parent)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	peerID, _, sender, vm, engine := setup(t, DefaultConfig(t))

	parent := snowmantest.BuildChild(snowmantest.Genesis)
	child := snowmantest.BuildChild(parent)

	var sendChitsCalled bool
	sender.SendChitsF = func(_ context.Context, _ ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDByHeight ids.ID, accepted ids.ID) {
		require.False(sendChitsCalled)
		sendChitsCalled = true
		require.Equal(uint32(15), requestID)
		require.Equal(snowmantest.GenesisID, preferredID)
		require.Equal(snowmantest.GenesisID, preferredIDByHeight)
		require.Equal(snowmantest.GenesisID, accepted)
	}

	var getBlockCalled bool
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		getBlockCalled = true

		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
			snowmantest.GenesisID,
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
		Beta:                  1,
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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blk0 := snowmantest.BuildChild(snowmantest.Genesis)
	blk1 := snowmantest.BuildChild(blk0)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	_, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(false)

	blk0 := snowmantest.BuildChild(snowmantest.Genesis)
	blk1 := snowmantest.BuildChild(blk0)

	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(false)

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	var sentPut bool
	sender.SendPutF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, blk []byte) {
		require.False(sentPut)
		sentPut = true

		require.Equal(vdr, nodeID)
		require.Equal(uint32(123), requestID)
		require.Equal(snowmantest.GenesisBytes, blk)
	}

	require.NoError(te.Get(context.Background(), vdr, 123, snowmantest.GenesisID))
	require.True(sentPut)
}

func TestEnginePushQuery(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, blk.Bytes()) {
			return blk, nil
		}
		return nil, errUnknownBytes
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		require.Equal(snowmantest.GenesisID, preferredID)
		require.Equal(snowmantest.GenesisID, preferredIDByHeight)
		require.Equal(snowmantest.GenesisID, acceptedID)
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
	vdr, _, sender, _, te := setup(t, DefaultConfig(t))

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
		Beta:                  1,
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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blk := snowmantest.BuildChild(snowmantest.Genesis)

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
		return snowmantest.GenesisID, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == snowmantest.GenesisID {
			return snowmantest.Genesis, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	blk := snowmantest.BuildChild(snowmantest.Genesis)

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
		return snowmantest.GenesisID, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == snowmantest.GenesisID {
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		require.Equal(snowmantest.GenesisBytes, b)
		return snowmantest.Genesis, nil
	}

	// Respond with an unexpected block and verify that the request is correctly
	// cleared.
	require.NoError(te.Put(context.Background(), vdr, reqID, snowmantest.GenesisBytes))
	require.Empty(te.blocked)
}

func TestEngineBlockingChitRequest(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	missingBlk := snowmantest.BuildChild(snowmantest.Genesis)
	parentBlk := snowmantest.BuildChild(missingBlk)
	blockingBlk := snowmantest.BuildChild(parentBlk)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	config := DefaultConfig(t)

	peerID, _, sender, vm, te := setup(t, config)

	sender.Default(true)

	issuedBlk := snowmantest.BuildChild(snowmantest.Genesis)

	missingBlk := snowmantest.BuildChild(snowmantest.Genesis)
	blockingBlk := snowmantest.BuildChild(missingBlk)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case issuedBlk.ID():
			return issuedBlk, nil
		case blockingBlk.ID():
			return blockingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(snowmantest.GenesisBytes, blkBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(issuedBlk.Bytes(), blkBytes):
			return issuedBlk, nil
		case bytes.Equal(missingBlk.Bytes(), blkBytes):
			return missingBlk, nil
		case bytes.Equal(blockingBlk.Bytes(), blkBytes):
			return blockingBlk, nil
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
		require.Equal(missingBlk.ID(), blkID)
	}

	// Issuing [blockingBlk] will register an issuer job for [blockingBlk]
	// awaiting on [missingBlk]. It will also send a request for [missingBlk].
	require.NoError(te.Put(
		context.Background(),
		peerID,
		0,
		blockingBlk.Bytes(),
	))

	var queryRequest *common.Request
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.Nil(queryRequest)
		require.Equal(set.Of(peerID), nodeIDs)
		queryRequest = &common.Request{
			NodeID:    peerID,
			RequestID: requestID,
		}
		require.Equal(issuedBlk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	// Issuing [issuedBlk] will immediately adds [issuedBlk] to consensus, sets
	// it as the preferred block, and sends a query for [issuedBlk].
	require.NoError(te.Put(
		context.Background(),
		peerID,
		0,
		issuedBlk.Bytes(),
	))

	sender.SendPullQueryF = nil

	// In response to the query for [issuedBlk], the peer is responding with,
	// the currently pending issuance, [blockingBlk]. The direct conflict of
	// [issuedBlk] is [missingBlk]. This registers a voter job dependent on
	// [blockingBlk] and [missingBlk].
	require.NoError(te.Chits(
		context.Background(),
		queryRequest.NodeID,
		queryRequest.RequestID,
		blockingBlk.ID(),
		missingBlk.ID(),
		blockingBlk.ID(),
	))
	require.Len(te.blocked, 2)

	queryRequest = nil
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.Nil(queryRequest)
		require.Equal(set.Of(peerID), nodeIDs)
		queryRequest = &common.Request{
			NodeID:    peerID,
			RequestID: requestID,
		}
		require.Equal(blockingBlk.ID(), blkID)
		require.Equal(uint64(1), requestedHeight)
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case issuedBlk.ID():
			return issuedBlk, nil
		case missingBlk.ID():
			return missingBlk, nil
		case blockingBlk.ID():
			return blockingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// Issuing [missingBlk] will add the block into consensus. However, it will
	// not send a query for it as it is not the preferred block.
	require.NoError(te.Put(
		context.Background(),
		getRequest.NodeID,
		getRequest.RequestID,
		missingBlk.Bytes(),
	))
	require.Equal(choices.Accepted, missingBlk.Status())
	require.Equal(choices.Accepted, blockingBlk.Status())
	require.Equal(choices.Rejected, issuedBlk.Status())
}

func TestEngineRetryFetch(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	missingBlk := snowmantest.BuildChild(snowmantest.Genesis)

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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	validBlk := snowmantest.BuildChild(snowmantest.Genesis)
	invalidBlk := snowmantest.BuildChild(validBlk)
	invalidBlk.VerifyV = errTest

	invalidBlkID := invalidBlk.ID()

	reqID := new(uint32)
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		*reqID = requestID
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	nodeID, _, sender, vm, te := setup(t, DefaultConfig(t))

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
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

	vdr, vdrs, sender, vm, te := setup(t, DefaultConfig(t))

	secondVdr := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(te.Ctx.SubnetID, secondVdr, nil, ids.Empty, 1))

	sender.Default(true)

	missingBlk := snowmantest.BuildChild(snowmantest.Genesis)
	pendingBlk := snowmantest.BuildChild(missingBlk)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	missingBlk := snowmantest.BuildChild(snowmantest.Genesis)
	pendingBlk := snowmantest.BuildChild(missingBlk)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
	engCfg.Params.Beta = 2

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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	pendingBlk := snowmantest.BuildChild(snowmantest.Genesis)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		Beta:                  1,
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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blk := snowmantest.BuildChild(snowmantest.Genesis)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(context.Background(), 0))

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blks := snowmantest.BuildDescendants(snowmantest.Genesis, 2)
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	acceptedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	rejectedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	pendingBlk := snowmantest.BuildChild(rejectedBlk)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	acceptedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	rejectedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	pendingBlk := snowmantest.BuildChild(rejectedBlk)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	acceptedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	rejectedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	pendingBlk := snowmantest.BuildChild(rejectedBlk)
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	acceptedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	rejectedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	rejectedBlk.VerifyV = errUnexpectedCall
	pendingBlk := snowmantest.BuildChild(rejectedBlk)
	pendingBlk.VerifyV = errUnexpectedCall

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	preferredBlk := snowmantest.BuildChild(snowmantest.Genesis)
	nonPreferredBlk := snowmantest.BuildChild(snowmantest.Genesis)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))
	expectedVdrSet := set.Of(vdr)

	blk1 := snowmantest.BuildChild(snowmantest.Genesis)
	// blk2 cannot pass verification until [blk1] has been marked as accepted.
	blk2 := snowmantest.BuildChild(blk1)
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))
	expectedVdrSet := set.Of(vdr)

	blk1 := snowmantest.BuildChild(snowmantest.Genesis)
	// blk2 cannot pass verification until [blk1] has been marked as accepted.
	blk2 := snowmantest.BuildChild(blk1)
	blk2.VerifyV = errInvalid
	blk3 := snowmantest.BuildChild(blk2)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

	vdr, _, sender, vm, te := setup(t, DefaultConfig(t))

	sender.Default(true)

	grandParentBlk := snowmantest.BuildChild(snowmantest.Genesis)

	parentBlkA := snowmantest.BuildChild(grandParentBlk)
	parentBlkA.VerifyV = errInvalid

	// Note that [parentBlkB] has the same [ID()] as [parentBlkA];
	// it's a different instantiation of the same block.
	parentBlkB := *parentBlkA
	parentBlkB.VerifyV = nil

	// Child of [parentBlkA]/[parentBlkB]
	childBlk := snowmantest.BuildChild(parentBlkA)

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(grandParentBlk.BytesV, b)
		return grandParentBlk, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		return &parentBlkB, nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case grandParentBlk.IDV:
			return grandParentBlk, nil
		case parentBlkB.IDV:
			return &parentBlkB, nil
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
		Beta:                  2,
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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)
	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blk := snowmantest.BuildChild(snowmantest.Genesis)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		Beta:                  1,
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
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)
	require.NoError(te.Start(context.Background(), 0))

	vm.LastAcceptedF = nil

	blk := snowmantest.BuildChild(snowmantest.Genesis)

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
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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

// Full blockchain structure:
//
//	  G
//	 / \
//	0   3
//	|   |
//	1   4
//	|
//	2
//
// K = 3, Alpha = 2, Beta = 1, ConcurrentRepolls = 1
//
// Initial configuration:
//
//	G
//	|
//	0
//	|
//	1
//	|
//	2
//
// The following is a regression test for a bug where the engine would stall.
//
//  1. Poll = 0: Handle a chit for block 1.
//  2. Poll = 0: Handle a chit for block 2.
//  3. Poll = 0: Handle a chit for block 3. This will issue a Get request for block 3. This will block on the issuance of block 3.
//  4. Attempt to issue block 4. This will block on the issuance of block 3.
//  5. Poll = 1: Handle a chit for block 1.
//  6. Poll = 1: Handle a chit for block 2.
//  7. Poll = 1: Handle a chit for block 4. This will block on the issuance of block 4.
//  8. Issue block 3.
//     Poll = 0 terminates. This will accept blocks 0 and 1. This will also reject block 3.
//     Block = 4 will attempt to be delivered, but because it is effectively rejected due to the acceptance of block 1, it will be dropped.
//     Poll = 1 should terminate and block 2 should be repolled.
func TestEngineVoteStallRegression(t *testing.T) {
	require := require.New(t)

	config := DefaultConfig(t)
	config.Params = snowball.Parameters{
		K:                     3,
		AlphaPreference:       2,
		AlphaConfidence:       2,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeIDs := []ids.NodeID{nodeID0, nodeID1, nodeID2}

	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, nodeID0, nil, ids.Empty, 1))
	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, nodeID1, nil, ids.Empty, 1))
	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, nodeID2, nil, ids.Empty, 1))

	sender := &common.SenderTest{
		T:          t,
		SendChitsF: func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID) {},
	}
	sender.Default(true)
	config.Sender = sender

	acceptedChain := snowmantest.BuildDescendants(snowmantest.Genesis, 3)
	rejectedChain := snowmantest.BuildDescendants(snowmantest.Genesis, 2)

	vm := &block.TestVM{
		TestVM: common.TestVM{
			T: t,
			InitializeF: func(
				context.Context,
				*snow.Context,
				database.Database,
				[]byte,
				[]byte,
				[]byte,
				chan<- common.Message,
				[]*common.Fx,
				common.AppSender,
			) error {
				return nil
			},
			SetStateF: func(context.Context, snow.State) error {
				return nil
			},
		},
		ParseBlockF: MakeParseBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
			acceptedChain,
			rejectedChain,
		),
		GetBlockF: MakeGetBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
			acceptedChain,
		),
		SetPreferenceF: func(context.Context, ids.ID) error {
			return nil
		},
		LastAcceptedF: MakeLastAcceptedBlockF(
			snowmantest.Genesis,
			acceptedChain,
		),
	}
	vm.Default(true)
	config.VM = vm

	engine, err := New(config)
	require.NoError(err)
	require.NoError(engine.Start(context.Background(), 0))

	var pollRequestIDs []uint32
	sender.SendPullQueryF = func(_ context.Context, polledNodeIDs set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		require.Equal(set.Of(nodeIDs...), polledNodeIDs)
		pollRequestIDs = append(pollRequestIDs, requestID)
	}

	// Issue block 0.
	require.NoError(engine.PushQuery(
		context.Background(),
		nodeID0,
		0,
		acceptedChain[0].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 1)

	// Issue block 1.
	require.NoError(engine.PushQuery(
		context.Background(),
		nodeID0,
		0,
		acceptedChain[1].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 2)

	// Issue block 2.
	require.NoError(engine.PushQuery(
		context.Background(),
		nodeID0,
		0,
		acceptedChain[2].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 3)

	// Apply votes in poll 0 to the blocks that will be accepted.
	require.NoError(engine.Chits(
		context.Background(),
		nodeID0,
		pollRequestIDs[0],
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
	))
	require.NoError(engine.Chits(
		context.Background(),
		nodeID1,
		pollRequestIDs[0],
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
	))

	// Attempt to apply votes in poll 0 for block 3. This will send a Get
	// request for block 3 and register the chits as a dependency on block 3.
	var getBlock3Request *common.Request
	sender.SendGetF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
		require.Nil(getBlock3Request)
		require.Equal(nodeID2, nodeID)
		getBlock3Request = &common.Request{
			NodeID:    nodeID,
			RequestID: requestID,
		}
		require.Equal(rejectedChain[0].ID(), blkID)
	}

	require.NoError(engine.Chits(
		context.Background(),
		nodeID2,
		pollRequestIDs[0],
		rejectedChain[0].ID(),
		rejectedChain[0].ID(),
		rejectedChain[0].ID(),
	))
	require.NotNil(getBlock3Request)

	// Attempt to issue block 4. This will register a dependency on block 3 for
	// the issuance of block 4.
	require.NoError(engine.PushQuery(
		context.Background(),
		nodeID0,
		0,
		rejectedChain[1].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 3)

	// Apply votes in poll 1 that will cause blocks 3 and 4 to be rejected once
	// poll 0 finishes.
	require.NoError(engine.Chits(
		context.Background(),
		nodeID0,
		pollRequestIDs[1],
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
	))
	require.NoError(engine.Chits(
		context.Background(),
		nodeID1,
		pollRequestIDs[1],
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
	))
	require.NoError(engine.Chits(
		context.Background(),
		nodeID2,
		pollRequestIDs[1],
		rejectedChain[1].ID(),
		rejectedChain[1].ID(),
		rejectedChain[1].ID(),
	))

	// Provide block 3.
	// This will cause poll 0 to terminate and accept blocks 0 and 1.
	// Then the engine will attempt to deliver block 4, but because block 1 is
	// accepted, block 4 will be dropped.
	// Then poll 1 should terminate because block 4 was dropped.
	vm.GetBlockF = MakeGetBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
		acceptedChain,
		rejectedChain,
	)

	require.NoError(engine.Put(
		context.Background(),
		getBlock3Request.NodeID,
		getBlock3Request.RequestID,
		rejectedChain[0].Bytes(),
	))
	require.Equal(choices.Accepted, acceptedChain[0].Status())
	require.Equal(choices.Accepted, acceptedChain[1].Status())
	require.Equal(choices.Processing, acceptedChain[2].Status())
	require.Equal(choices.Rejected, rejectedChain[0].Status())

	// Then engine should issue as many queries as needed to confirm block 2.
	for i := 2; i < len(pollRequestIDs); i++ {
		for _, nodeID := range nodeIDs {
			require.NoError(engine.Chits(
				context.Background(),
				nodeID,
				pollRequestIDs[i],
				acceptedChain[2].ID(),
				acceptedChain[2].ID(),
				acceptedChain[2].ID(),
			))
		}
	}
	require.Equal(choices.Accepted, acceptedChain[0].Status())
	require.Equal(choices.Accepted, acceptedChain[1].Status())
	require.Equal(choices.Accepted, acceptedChain[2].Status())
	require.Equal(choices.Rejected, rejectedChain[0].Status())
}

func TestGetProcessingAncestor(t *testing.T) {
	var (
		ctx = snowtest.ConsensusContext(
			snowtest.Context(t, snowtest.PChainID),
		)
		issuedBlock   = snowmantest.BuildChild(snowmantest.Genesis)
		unissuedBlock = snowmantest.BuildChild(issuedBlock)
	)

	metrics, err := newMetrics(prometheus.NewRegistry())
	require.NoError(t, err)

	c := &snowman.Topological{}
	require.NoError(t, c.Initialize(
		ctx,
		snowball.DefaultParameters,
		snowmantest.GenesisID,
		0,
		time.Now(),
	))

	require.NoError(t, c.Add(context.Background(), issuedBlock))

	nonVerifiedAncestors := ancestor.NewTree()
	nonVerifiedAncestors.Add(unissuedBlock.ID(), unissuedBlock.Parent())

	tests := []struct {
		name             string
		engine           *Transitive
		initialVote      ids.ID
		expectedAncestor ids.ID
		expectedFound    bool
	}{
		{
			name: "drop accepted blockID",
			engine: &Transitive{
				Config: Config{
					Ctx: ctx,
					VM: &block.TestVM{
						TestVM: common.TestVM{
							T: t,
						},
						GetBlockF: MakeGetBlockF(
							[]*snowmantest.Block{snowmantest.Genesis},
						),
					},
					Consensus: c,
				},
				metrics:          metrics,
				nonVerifieds:     ancestor.NewTree(),
				pending:          map[ids.ID]snowman.Block{},
				nonVerifiedCache: &cache.Empty[ids.ID, snowman.Block]{},
			},
			initialVote:      snowmantest.GenesisID,
			expectedAncestor: ids.Empty,
			expectedFound:    false,
		},
		{
			name: "return processing blockID",
			engine: &Transitive{
				Config: Config{
					Ctx: ctx,
					VM: &block.TestVM{
						TestVM: common.TestVM{
							T: t,
						},
						GetBlockF: MakeGetBlockF(
							[]*snowmantest.Block{snowmantest.Genesis},
						),
					},
					Consensus: c,
				},
				metrics:          metrics,
				nonVerifieds:     ancestor.NewTree(),
				pending:          map[ids.ID]snowman.Block{},
				nonVerifiedCache: &cache.Empty[ids.ID, snowman.Block]{},
			},
			initialVote:      issuedBlock.ID(),
			expectedAncestor: issuedBlock.ID(),
			expectedFound:    true,
		},
		{
			name: "drop unknown blockID",
			engine: &Transitive{
				Config: Config{
					Ctx: ctx,
					VM: &block.TestVM{
						TestVM: common.TestVM{
							T: t,
						},
						GetBlockF: MakeGetBlockF(
							[]*snowmantest.Block{snowmantest.Genesis},
						),
					},
					Consensus: c,
				},
				metrics:          metrics,
				nonVerifieds:     ancestor.NewTree(),
				pending:          map[ids.ID]snowman.Block{},
				nonVerifiedCache: &cache.Empty[ids.ID, snowman.Block]{},
			},
			initialVote:      ids.GenerateTestID(),
			expectedAncestor: ids.Empty,
			expectedFound:    false,
		},
		{
			name: "apply vote through ancestor tree",
			engine: &Transitive{
				Config: Config{
					Ctx: ctx,
					VM: &block.TestVM{
						TestVM: common.TestVM{
							T: t,
						},
						GetBlockF: MakeGetBlockF(
							[]*snowmantest.Block{snowmantest.Genesis},
						),
					},
					Consensus: c,
				},
				metrics:          metrics,
				nonVerifieds:     nonVerifiedAncestors,
				pending:          map[ids.ID]snowman.Block{},
				nonVerifiedCache: &cache.Empty[ids.ID, snowman.Block]{},
			},
			initialVote:      unissuedBlock.ID(),
			expectedAncestor: issuedBlock.ID(),
			expectedFound:    true,
		},
		{
			name: "apply vote through pending set",
			engine: &Transitive{
				Config: Config{
					Ctx: ctx,
					VM: &block.TestVM{
						TestVM: common.TestVM{
							T: t,
						},
						GetBlockF: MakeGetBlockF(
							[]*snowmantest.Block{snowmantest.Genesis},
						),
					},
					Consensus: c,
				},
				metrics:      metrics,
				nonVerifieds: ancestor.NewTree(),
				pending: map[ids.ID]snowman.Block{
					unissuedBlock.ID(): unissuedBlock,
				},
				nonVerifiedCache: &cache.Empty[ids.ID, snowman.Block]{},
			},
			initialVote:      unissuedBlock.ID(),
			expectedAncestor: issuedBlock.ID(),
			expectedFound:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			ancestor, found := test.engine.getProcessingAncestor(context.Background(), test.initialVote)
			require.Equal(test.expectedAncestor, ancestor)
			require.Equal(test.expectedFound, found)
		})
	}
}
