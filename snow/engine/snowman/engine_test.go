// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/ancestor"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	snowmanenginetest "github.com/ava-labs/avalanchego/snow/engine/snowman/enginetest"
)

var (
	errUnknownBlock = errors.New("unknown block")
	errUnknownBytes = errors.New("unknown bytes")
	errInvalid      = errors.New("invalid")
	errTest         = errors.New("non-nil test")
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

func setup(t *testing.T, config Config) (ids.NodeID, validators.Manager, *enginetest.Sender, *blocktest.VM, *Engine) {
	require := require.New(t)

	vdr := ids.GenerateTestNodeID()
	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, vdr, nil, ids.Empty, 1))
	require.NoError(config.ConnectedValidators.Connected(t.Context(), vdr, version.Current))
	config.Validators.RegisterSetCallbackListener(config.Ctx.SubnetID, config.ConnectedValidators)

	sender := &enginetest.Sender{T: t}
	config.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
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

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
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

	require.NoError(te.Start(t.Context(), 0))

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
	require.NoError(engine.Put(t.Context(), peerID, 0, child.Bytes()))
	require.NotNil(request)
	require.Equal(1, engine.blocked.NumDependencies())

	vm.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		return nil, errUnknownBytes
	}

	// Because this request doesn't provide [parent], the [child] job should be
	// cancelled.
	require.NoError(engine.Put(t.Context(), request.NodeID, request.RequestID, nil))
	require.Zero(engine.blocked.NumDependencies())
}

func TestEngineQuery(t *testing.T) {
	require := require.New(t)

	peerID, _, sender, vm, engine := setup(t, DefaultConfig(t))

	parent := snowmantest.BuildChild(snowmantest.Genesis)
	child := snowmantest.BuildChild(parent)

	var sendChitsCalled bool
	sender.SendChitsF = func(_ context.Context, _ ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDByHeight ids.ID, accepted ids.ID, acceptedHeight uint64) {
		require.False(sendChitsCalled)
		sendChitsCalled = true
		require.Equal(uint32(15), requestID)
		require.Equal(snowmantest.GenesisID, preferredID)
		require.Equal(snowmantest.GenesisID, preferredIDByHeight)
		require.Equal(snowmantest.GenesisID, accepted)
		require.Equal(uint64(0), acceptedHeight)
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
	require.NoError(engine.PullQuery(t.Context(), peerID, 15, parent.ID(), 1))
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
		require.Equal(parent.Height()+1, requestedHeight) // next preferred height
	}

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(parent.Bytes(), b)
		return parent, nil
	}

	// After receiving [parent], the engine will parse it, issue it, and then
	// send a pull query.
	require.NoError(engine.Put(t.Context(), getRequest.NodeID, getRequest.RequestID, parent.Bytes()))
	require.NotNil(queryRequest)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case parent.ID(), child.ID():
		default:
			t.Fatal(errUnknownBlock)
		}
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
	require.NoError(engine.Chits(t.Context(), queryRequest.NodeID, queryRequest.RequestID, child.ID(), child.ID(), child.ID(), child.Height()))

	queryRequest = nil
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blockID ids.ID, requestedHeight uint64) {
		require.Nil(queryRequest)
		require.Equal(set.Of(peerID), nodeIDs)
		queryRequest = &common.Request{
			NodeID:    peerID,
			RequestID: requestID,
		}
		require.Equal(child.ID(), blockID)
		require.Equal(child.Height()+1, requestedHeight) // next preferred height
	}

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(child.Bytes(), b)

		vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			switch blkID {
			case parent.ID():
				return parent, nil
			case child.ID():
				return child, nil
			default:
				t.Fatal(errUnknownBlock)
			}
			return nil, errUnknownBlock
		}

		return child, nil
	}

	// After receiving [child], the engine will parse it, issue it, and then
	// apply the votes received during the poll for [parent]. Applying the votes
	// should cause both [parent] and [child] to be accepted.
	require.NoError(engine.Put(t.Context(), getRequest.NodeID, getRequest.RequestID, child.Bytes()))
	require.Equal(snowtest.Accepted, parent.Status)
	require.Equal(snowtest.Accepted, child.Status)
	require.Zero(engine.blocked.NumDependencies())
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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

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
		require.Equal(blk0.Height()+1, requestedHeight) // next preferred height
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
		t.Context(),
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
		default:
			t.Fatal(errUnknownBlock)
		}
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
	require.NoError(te.Chits(t.Context(), vdr0, *queryRequestID, blk1.ID(), blk1.ID(), blk1.ID(), blk1.Height()))
	require.NoError(te.Chits(t.Context(), vdr1, *queryRequestID, blk1.ID(), blk1.ID(), blk1.ID(), blk1.Height()))

	vm.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			switch {
			case blkID == blk0.ID():
				return blk0, nil
			case blkID == blk1.ID():
				return blk1, nil
			default:
				t.Fatal(errUnknownBlock)
			}
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
		require.Equal(blk1.Height()+1, requestedHeight) // next preferred height
	}
	require.NoError(te.Put(t.Context(), vdr0, *getRequestID, blk1.Bytes()))

	// Should be dropped because the query was already filled
	require.NoError(te.Chits(t.Context(), vdr2, *queryRequestID, blk0.ID(), blk0.ID(), blk0.ID(), blk0.Height()))

	require.Equal(snowtest.Accepted, blk1.Status)
	require.Zero(te.blocked.NumDependencies())
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
		t.Context(),
		te.Ctx.NodeID,
		blk1,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.NoError(te.issue(
		t.Context(),
		te.Ctx.NodeID,
		blk0,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	pref, _ := te.Consensus.Preference()
	require.Equal(blk1.ID(), pref)
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

	require.NoError(te.Get(t.Context(), vdr, 123, snowmantest.GenesisID))
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
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDByHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) {
		require.False(*chitted)
		*chitted = true
		require.Equal(vdr, inVdr)
		require.Equal(uint32(20), requestID)
		require.Equal(snowmantest.GenesisID, preferredID)
		require.Equal(snowmantest.GenesisID, preferredIDByHeight)
		require.Equal(snowmantest.GenesisID, acceptedID)
		require.Equal(uint64(0), acceptedHeight)
	}

	queried := new(bool)
	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], _ uint32, blkID ids.ID, requestedHeight uint64) {
		require.False(*queried)
		*queried = true
		vdrSet := set.Of(vdr)
		require.True(inVdrs.Equals(vdrSet))
		require.Equal(blk.ID(), blkID)
		require.Equal(blk.Height()+1, requestedHeight) // next preferred height
	}

	require.NoError(te.PushQuery(t.Context(), vdr, 20, blk.Bytes(), 1))

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
		t.Fatal("should not be sending pulls when we are the block producer")
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
	require.NoError(te.Notify(t.Context(), common.PendingTxs))

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

	te.repoll(t.Context())

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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

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
		require.Equal(blk.Height()+1, requestedHeight) // next preferred height
	}

	require.NoError(te.issue(
		t.Context(),
		te.Ctx.NodeID,
		blk,
		true,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.Equal(1, te.polls.Len())

	require.NoError(te.QueryFailed(t.Context(), vdr0, *queryRequestID))

	require.Equal(1, te.polls.Len())

	repolled := new(bool)
	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID, uint64) {
		*repolled = true
	}
	require.NoError(te.QueryFailed(t.Context(), vdr1, *queryRequestID))

	require.True(*repolled)
}

func TestEngineNoQuery(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == snowmantest.GenesisID {
			return snowmantest.Genesis, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	require.NoError(te.issue(
		t.Context(),
		te.Ctx.NodeID,
		blk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
}

func TestEngineNoRepollQuery(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == snowmantest.GenesisID {
			return snowmantest.Genesis, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

	te.repoll(t.Context())
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

	require.NoError(te.PullQuery(t.Context(), vdr, 0, blkID, 0))

	require.Equal(1, te.blkReqs.Len())

	require.NoError(te.GetFailed(t.Context(), vdr, *reqID))

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
		default:
			t.Fatal(errUnknownBlock)
		}
		return nil, errUnknownBlock
	}

	var reqID uint32
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		reqID = requestID
	}

	require.NoError(te.issue(
		t.Context(),
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
	require.NoError(te.Chits(t.Context(), vdr, reqID, fakeBlkID, fakeBlkID, fakeBlkID, blk.Height()))
	require.Equal(1, te.blocked.NumDependencies())

	sender.CantSendPullQuery = false

	require.NoError(te.GetFailed(t.Context(), vdr, reqID))
	require.Zero(te.blocked.NumDependencies())
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
		default:
			t.Fatal(errUnknownBlock)
		}
		return nil, errUnknownBlock
	}

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte, _ uint64) {
		reqID = requestID
	}

	require.NoError(te.issue(
		t.Context(),
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
	require.NoError(te.Chits(t.Context(), vdr, reqID, fakeBlkID, fakeBlkID, fakeBlkID, blk.Height()))
	require.Equal(1, te.blocked.NumDependencies())

	sender.CantSendPullQuery = false

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisBytes, b)
		return snowmantest.Genesis, nil
	}

	// Respond with an unexpected block and verify that the request is correctly
	// cleared.
	require.NoError(te.Put(t.Context(), vdr, reqID, snowmantest.GenesisBytes))
	require.Zero(te.blocked.NumDependencies())
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
		t.Context(),
		te.Ctx.NodeID,
		parentBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	sender.CantSendChits = false

	require.NoError(te.PushQuery(t.Context(), vdr, 0, blockingBlk.Bytes(), 0))

	require.Equal(2, te.blocked.NumDependencies())

	sender.CantSendPullQuery = false

	require.NoError(te.issue(
		t.Context(),
		te.Ctx.NodeID,
		missingBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))

	require.Zero(te.blocked.NumDependencies())
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
		t.Context(),
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
		require.Equal(issuedBlk.Height()+1, requestedHeight) // next preferred height
	}

	// Issuing [issuedBlk] will immediately adds [issuedBlk] to consensus, sets
	// it as the preferred block, and sends a query for [issuedBlk].
	require.NoError(te.Put(
		t.Context(),
		peerID,
		0,
		issuedBlk.Bytes(),
	))

	sender.SendPullQueryF = nil

	// In response to the query for [issuedBlk], the peer responds with tip
	// preference [blockingBlk] and preference-at-requested-height [missingBlk].
	// The engine only acts on the block at the requested height, so it registers
	// a voter job dependent solely on [missingBlk], which is still being fetched.
	require.NoError(te.Chits(
		t.Context(),
		queryRequest.NodeID,
		queryRequest.RequestID,
		blockingBlk.ID(),
		missingBlk.ID(),
		blockingBlk.ID(),
		blockingBlk.Height(),
	))
	require.Equal(1, te.blocked.NumDependencies())

	queryRequest = nil
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		require.Nil(queryRequest)
		require.Equal(set.Of(peerID), nodeIDs)
		queryRequest = &common.Request{
			NodeID:    peerID,
			RequestID: requestID,
		}
		require.Equal(blockingBlk.ID(), blkID)
		require.Equal(blockingBlk.Height()+1, requestedHeight) // next preferred height
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

	// Delivering [missingBlk] adds it to consensus and resolves the voter's only
	// dependency, so the vote for [missingBlk] (the peer's preference at the
	// requested height) is applied: [missingBlk] is accepted and its conflict
	// [issuedBlk] is rejected. [blockingBlk] becomes the new preferred block and
	// is queried, but is not accepted - the vote was only for [missingBlk], not
	// the tip.
	require.NoError(te.Put(
		t.Context(),
		getRequest.NodeID,
		getRequest.RequestID,
		missingBlk.Bytes(),
	))
	require.Equal(snowtest.Accepted, missingBlk.Status)
	require.Equal(snowtest.Rejected, issuedBlk.Status)
	require.Equal(snowtest.Undecided, blockingBlk.Status)
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

	require.NoError(te.PullQuery(t.Context(), vdr, 0, missingBlk.ID(), 0))

	vm.CantGetBlock = true
	sender.SendGetF = nil

	require.NoError(te.GetFailed(t.Context(), vdr, *reqID))

	vm.CantGetBlock = false

	called := new(bool)
	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {
		*called = true
	}

	require.NoError(te.PullQuery(t.Context(), vdr, 0, missingBlk.ID(), 0))

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
		t.Context(),
		te.Ctx.NodeID,
		validBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
	sender.SendPushQueryF = nil
	require.NoError(te.issue(
		t.Context(),
		te.Ctx.NodeID,
		invalidBlk,
		false,
		te.metrics.issued.WithLabelValues(unknownSource),
	))
	require.NoError(te.Chits(t.Context(), vdr, *reqID, invalidBlkID, invalidBlkID, invalidBlkID, invalidBlk.Height()))

	require.Equal(snowtest.Accepted, validBlk.Status)
}

func TestEngineGossip(t *testing.T) {
	require := require.New(t)

	nodeID, _, sender, vm, te := setup(t, DefaultConfig(t))

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	var calledSendPullQuery bool
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ ids.ID, _ uint64) {
		calledSendPullQuery = true
		require.Equal(set.Of(nodeID), nodeIDs)
	}

	require.NoError(te.Gossip(t.Context()))

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

	require.NoError(te.PushQuery(t.Context(), vdr, 0, pendingBlk.Bytes(), 0))

	require.NoError(te.Put(t.Context(), secondVdr, *reqID, []byte{3}))

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

	require.NoError(te.Put(t.Context(), vdr, *reqID, missingBlk.Bytes()))

	pref, _ := te.Consensus.Preference()
	require.Equal(pendingBlk.ID(), pref)
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

	require.NoError(te.PushQuery(t.Context(), vdr, 0, pendingBlk.Bytes(), 0))

	sender.SendGetF = nil
	sender.CantSendGet = false

	require.NoError(te.PushQuery(t.Context(), vdr, *reqID, []byte{3}, 0))

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

	require.NoError(te.Put(t.Context(), vdr, *reqID, missingBlk.Bytes()))

	pref, _ := te.Consensus.Preference()
	require.Equal(pendingBlk.ID(), pref)
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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

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

	require.NoError(te.Put(t.Context(), vdr, 0, pendingBlk.Bytes()))

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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

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
		require.Equal(blk.Height()+1, requestedHeight) // next preferred height
	}
	require.NoError(te.issue(
		t.Context(),
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
		default:
			t.Fatal(errUnknownBlock)
		}
		return nil, errUnknownBlock
	}

	require.Equal(snowtest.Undecided, blk.Status)

	require.NoError(te.Chits(t.Context(), vdr0, *queryRequestID, blk.ID(), blk.ID(), blk.ID(), blk.Height()))
	require.Equal(snowtest.Undecided, blk.Status)

	require.NoError(te.Chits(t.Context(), vdr0, *queryRequestID, blk.ID(), blk.ID(), blk.ID(), blk.Height()))
	require.Equal(snowtest.Undecided, blk.Status)

	require.NoError(te.Chits(t.Context(), vdr1, *queryRequestID, blk.ID(), blk.ID(), blk.ID(), blk.Height()))
	require.Equal(snowtest.Accepted, blk.Status)
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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)

	require.NoError(te.Start(t.Context(), 0))

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
	require.NoError(te.Notify(t.Context(), common.PendingTxs))

	require.True(queried)

	queried = false
	require.NoError(te.Notify(t.Context(), common.PendingTxs))

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

	require.NoError(te.Chits(t.Context(), vdr, reqID, blk0.ID(), blk0.ID(), blk0.ID(), blk0.Height()))

	require.True(queried)
}

func TestEngineDropRejectedBlockOnReceipt(t *testing.T) {
	require := require.New(t)

	nodeID, _, sender, vm, te := setup(t, DefaultConfig(t))

	// Ignore outbound chits
	sender.SendChitsF = func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID, uint64) {}

	acceptedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	rejectedChain := snowmantest.BuildDescendants(snowmantest.Genesis, 2)
	vm.ParseBlockF = MakeParseBlockF(
		[]*snowmantest.Block{
			snowmantest.Genesis,
			acceptedBlk,
		},
		rejectedChain,
	)
	vm.GetBlockF = MakeGetBlockF([]*snowmantest.Block{
		snowmantest.Genesis,
		acceptedBlk,
	})

	// Track outbound queries
	var queryRequestIDs []uint32
	sender.SendPullQueryF = func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		require.Equal(set.Of(nodeID), nodeIDs)
		queryRequestIDs = append(queryRequestIDs, requestID)
	}

	// Issue [acceptedBlk] to the engine. This
	require.NoError(te.PushQuery(t.Context(), nodeID, 0, acceptedBlk.Bytes(), acceptedBlk.Height()))
	require.Len(queryRequestIDs, 1)

	// Vote for [acceptedBlk] and cause it to be accepted.
	require.NoError(te.Chits(t.Context(), nodeID, queryRequestIDs[0], acceptedBlk.ID(), acceptedBlk.ID(), acceptedBlk.ID(), acceptedBlk.Height()))
	require.Len(queryRequestIDs, 1) // Shouldn't have caused another query
	require.Equal(snowtest.Accepted, acceptedBlk.Status)

	// Attempt to issue rejectedChain[1] to the engine. This should be dropped
	// because the engine knows it has rejected it's parent rejectedChain[0].
	require.NoError(te.PushQuery(t.Context(), nodeID, 0, rejectedChain[1].Bytes(), acceptedBlk.Height()))
	require.Len(queryRequestIDs, 1) // Shouldn't have caused another query
	require.Zero(te.blkReqs.Len())
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
			t.Fatal(errUnknownBlock)
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
		require.Equal(preferredBlk.Height()+1, requestedHeight) // next preferred height
	}
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, blkID ids.ID, requestedHeight uint64) {
		require.NotEqual(nonPreferredBlk.ID(), blkID)
		require.Equal(preferredBlk.Height()+1, requestedHeight) // next preferred height
	}

	require.NoError(te.Put(t.Context(), vdr, 0, preferredBlk.Bytes()))

	require.NoError(te.Put(t.Context(), vdr, 0, nonPreferredBlk.Bytes()))
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
			t.Fatal(errUnknownBlock)
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
	require.NoError(te.PushQuery(t.Context(), vdr, 0, blk2.Bytes(), 0))
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
		require.Equal(blk1.Height()+1, requestedHeight) // next preferred height
	}
	// This engine now handles the response to the "Get" request. This should cause [blk1] to be issued
	// which will result in attempting to issue [blk2]. However, [blk2] should fail verification and be dropped.
	// By issuing [blk1], this node should fire a "PullQuery" request for [blk1].
	// (see above for expected "PullQuery" request)
	require.NoError(te.Put(t.Context(), vdr, *reqID, blk1.Bytes()))
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
	require.NoError(te.Chits(t.Context(), vdr, *queryRequestID, blk2.ID(), blk2.ID(), blk2.ID(), blk2.Height()))

	// The votes should be bubbled through [blk2] despite the fact that it is
	// failing verification.
	require.NoError(te.Put(t.Context(), *reqVdr, *sendReqID, blk2.Bytes()))

	// The vote should be bubbled through [blk2], such that [blk1] gets marked as Accepted.
	require.Equal(snowtest.Accepted, blk1.Status)
	require.Equal(snowtest.Undecided, blk2.Status)

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
		require.Equal(blk2.Height()+1, requestedHeight) // next preferred height
	}
	// Expect that the Engine will send a PullQuery after receiving this Gossip message for [blk2].
	require.NoError(te.PushQuery(t.Context(), vdr, 0, blk2.Bytes(), 0))
	require.True(*queried)

	// After a single vote for [blk2], it should be marked as accepted.
	require.NoError(te.Chits(t.Context(), vdr, *queryRequestID, blk2.ID(), blk2.ID(), blk2.ID(), blk2.Height()))
	require.Equal(snowtest.Accepted, blk2.Status)
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
			t.Fatal(errUnknownBlock)
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
	require.NoError(te.PushQuery(t.Context(), vdr, 0, blk3.Bytes(), 0))
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
		require.Equal(blk1.Height()+1, requestedHeight) // next preferred height
	}

	// Answer the request, this should result in [blk1] being issued as well.
	require.NoError(te.Put(t.Context(), vdr, *reqID, blk2.Bytes()))
	require.True(*queried)

	sendReqID := new(uint32)
	reqVdr := new(ids.NodeID)
	// Update GetF to produce a more detailed error message in the case that receiving a Chits
	// message causes us to send another Get request.
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		switch blkID {
		case blk1.ID():
			t.Fatal("Unexpectedly sent a Get request for blk1")
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
			t.Fatal("Unexpectedly sent a Get request for unknown block")
		}
	}

	// Now we are expecting a Chits message and we receive it for [blk3].
	// This will cause the node to again request [blk3].
	require.NoError(te.Chits(t.Context(), vdr, *queryRequestID, blk3.ID(), blk1.ID(), blk3.ID(), blk3.Height()))

	// Drop the re-request for [blk3] to cause the poll to terminate. The votes
	// should be bubbled through [blk3] despite the fact that it hasn't been
	// issued.
	require.NoError(te.GetFailed(t.Context(), *reqVdr, *sendReqID))

	// The vote should be bubbled through [blk3] and [blk2] such that [blk1]
	// gets marked as Accepted.
	require.Equal(snowtest.Accepted, blk1.Status)
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
		require.Equal(grandParentBlk.Height()+1, requestedHeight) // next preferred height
	}

	// Give the engine the grandparent
	require.NoError(te.Put(t.Context(), vdr, 0, grandParentBlk.BytesV))

	vm.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(parentBlkA.BytesV, b)
		return parentBlkA, nil
	}

	// Give the node [parentBlkA]/[parentBlkB].
	// When it's parsed we get [parentBlkA] (not [parentBlkB]).
	// [parentBlkA] fails verification and gets put into [te.nonVerifiedCache].
	require.NoError(te.Put(t.Context(), vdr, 0, parentBlkA.BytesV))

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
		require.Equal(parentBlkA.Height()+1, requestedHeight) // next preferred height
	}
	sender.CantSendPullQuery = false

	// Give the engine [parentBlkA]/[parentBlkB] again.
	// This time when we parse it we get [parentBlkB] (not [parentBlkA]).
	// When we fetch it using [GetBlockF] we get [parentBlkB].
	// Note that [parentBlkB] doesn't fail verification and is issued into consensus.
	// This evicts [parentBlkA] from [te.nonVerifiedCache].
	require.NoError(te.Put(t.Context(), vdr, 0, parentBlkA.BytesV))

	// Give 2 chits for [parentBlkA]/[parentBlkB]
	require.NoError(te.Chits(t.Context(), vdr, *queryRequestAID, parentBlkB.IDV, parentBlkB.IDV, parentBlkB.IDV, parentBlkB.Height()))
	require.NoError(te.Chits(t.Context(), vdr, *queryRequestGPID, parentBlkB.IDV, parentBlkB.IDV, parentBlkB.IDV, parentBlkB.Height()))

	// Assert that the blocks' statuses are correct.
	// The evicted [parentBlkA] shouldn't be changed.
	require.Equal(snowtest.Undecided, parentBlkA.Status)
	require.Equal(snowtest.Accepted, parentBlkB.Status)

	vm.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return childBlk, nil
	}

	sentQuery := new(bool)
	sender.SendPushQueryF = func(context.Context, set.Set[ids.NodeID], uint32, []byte, uint64) {
		*sentQuery = true
	}

	// Should issue a new block and send a query for it.
	require.NoError(te.Notify(t.Context(), common.PendingTxs))
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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)
	require.NoError(te.Start(t.Context(), 0))

	vm.LastAcceptedF = nil

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkBytes []byte, requestedHeight uint64) {
		*queryRequestID = requestID
		require.Contains(inVdrs, vdr)
		require.Equal(blk.Bytes(), blkBytes)
		require.Equal(blk.Height()+1, requestedHeight) // next preferred height
	}

	require.NoError(te.issue(
		t.Context(),
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
		default:
			t.Fatal(errUnknownBlock)
		}
		return nil, errUnknownBlock
	}

	require.Equal(snowtest.Undecided, blk.Status)

	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
		*queryRequestID = requestID
		require.Contains(inVdrs, vdr)
		require.Equal(blk.ID(), blkID)
		require.Equal(blk.Height()+1, requestedHeight) // next preferred height
	}

	require.NoError(te.Chits(t.Context(), vdr, *queryRequestID, blk.ID(), blk.ID(), blk.ID(), blk.Height()))

	require.Equal(snowtest.Undecided, blk.Status)

	require.NoError(te.QueryFailed(t.Context(), vdr, *queryRequestID))

	require.Equal(snowtest.Accepted, blk.Status)
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

	sender := &enginetest.Sender{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &blocktest.VM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, id)
		return snowmantest.Genesis, nil
	}

	te, err := New(engCfg)
	require.NoError(err)
	require.NoError(te.Start(t.Context(), 0))

	vm.LastAcceptedF = nil

	blk := snowmantest.BuildChild(snowmantest.Genesis)

	// Issue the block. This shouldn't call the sender, because creating the
	// poll should fail.
	require.NoError(te.issue(
		t.Context(),
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
		require.Equal(blk.Height()+1, requestedHeight) // next preferred height
		queried = true
	}

	// Because there is now a validator that can be queried, gossip should
	// trigger creation of the poll.
	require.NoError(te.Gossip(t.Context()))
	require.True(queried)

	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case blk.ID():
			return blk, nil
		default:
			t.Fatal(errUnknownBlock)
		}
		return nil, errUnknownBlock
	}

	// Voting for the block that was issued during the period when the validator
	// set was misconfigured should result in it being accepted successfully.
	require.NoError(te.Chits(t.Context(), vdr, queryRequestID, blk.ID(), blk.ID(), blk.ID(), blk.Height()))
	require.Equal(snowtest.Accepted, blk.Status)
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

	sender := &enginetest.Sender{
		T:          t,
		SendChitsF: func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID, uint64) {},
	}
	sender.Default(true)
	config.Sender = sender

	acceptedChain := snowmantest.BuildDescendants(snowmantest.Genesis, 3)
	rejectedChain := snowmantest.BuildDescendants(snowmantest.Genesis, 2)

	vm := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(
				context.Context,
				*snow.Context,
				database.Database,
				[]byte,
				[]byte,
				[]byte,
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
		LastAcceptedF: snowmantest.MakeLastAcceptedBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
			acceptedChain,
		),
	}
	vm.Default(true)
	config.VM = vm

	engine, err := New(config)
	require.NoError(err)
	require.NoError(engine.Start(t.Context(), 0))

	var pollRequestIDs []uint32
	sender.SendPullQueryF = func(_ context.Context, polledNodeIDs set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		require.Equal(set.Of(nodeIDs...), polledNodeIDs)
		pollRequestIDs = append(pollRequestIDs, requestID)
	}

	// Issue block 0.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID0,
		0,
		acceptedChain[0].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 1)

	// Issue block 1.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID0,
		0,
		acceptedChain[1].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 2)

	// Issue block 2.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID0,
		0,
		acceptedChain[2].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 3)

	// Apply votes in poll 0 to the blocks that will be accepted.
	require.NoError(engine.Chits(
		t.Context(),
		nodeID0,
		pollRequestIDs[0],
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		acceptedChain[1].Height(),
	))
	require.NoError(engine.Chits(
		t.Context(),
		nodeID1,
		pollRequestIDs[0],
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
		acceptedChain[2].Height(),
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
		t.Context(),
		nodeID2,
		pollRequestIDs[0],
		rejectedChain[0].ID(),
		rejectedChain[0].ID(),
		rejectedChain[0].ID(),
		rejectedChain[0].Height(),
	))
	require.NotNil(getBlock3Request)

	// Attempt to issue block 4. This will register a dependency on block 3 for
	// the issuance of block 4.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID0,
		0,
		rejectedChain[1].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 3)

	// Apply votes in poll 1 that will cause blocks 3 and 4 to be rejected once
	// poll 0 finishes.
	require.NoError(engine.Chits(
		t.Context(),
		nodeID0,
		pollRequestIDs[1],
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		acceptedChain[1].Height(),
	))
	require.NoError(engine.Chits(
		t.Context(),
		nodeID1,
		pollRequestIDs[1],
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
		acceptedChain[2].ID(),
		acceptedChain[2].Height(),
	))
	require.NoError(engine.Chits(
		t.Context(),
		nodeID2,
		pollRequestIDs[1],
		rejectedChain[1].ID(),
		rejectedChain[1].ID(),
		rejectedChain[1].ID(),
		rejectedChain[1].Height(),
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
		t.Context(),
		getBlock3Request.NodeID,
		getBlock3Request.RequestID,
		rejectedChain[0].Bytes(),
	))
	require.Equal(snowtest.Accepted, acceptedChain[0].Status)
	require.Equal(snowtest.Accepted, acceptedChain[1].Status)
	require.Equal(snowtest.Undecided, acceptedChain[2].Status)
	require.Equal(snowtest.Rejected, rejectedChain[0].Status)

	// Then engine should issue as many queries as needed to confirm block 2.
	for i := 2; i < len(pollRequestIDs); i++ {
		for _, nodeID := range nodeIDs {
			require.NoError(engine.Chits(
				t.Context(),
				nodeID,
				pollRequestIDs[i],
				acceptedChain[2].ID(),
				acceptedChain[2].ID(),
				acceptedChain[2].ID(),
				acceptedChain[2].Height(),
			))
		}
	}
	require.Equal(snowtest.Accepted, acceptedChain[0].Status)
	require.Equal(snowtest.Accepted, acceptedChain[1].Status)
	require.Equal(snowtest.Accepted, acceptedChain[2].Status)
	require.Equal(snowtest.Rejected, rejectedChain[0].Status)
}

// When a voter is registered with multiple dependencies, the engine must not
// execute the voter until all of the dependencies have been resolved; even if
// one of the dependencies has been abandoned.
func TestEngineEarlyTerminateVoterRegression(t *testing.T) {
	require := require.New(t)

	config := DefaultConfig(t)
	nodeID := ids.GenerateTestNodeID()
	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, nodeID, nil, ids.Empty, 1))

	sender := &enginetest.Sender{
		T:          t,
		SendChitsF: func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID, uint64) {},
	}
	sender.Default(true)
	config.Sender = sender

	chain := snowmantest.BuildDescendants(snowmantest.Genesis, 3)
	vm := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(
				context.Context,
				*snow.Context,
				database.Database,
				[]byte,
				[]byte,
				[]byte,
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
			chain,
		),
		GetBlockF: MakeGetBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
		),
		SetPreferenceF: func(context.Context, ids.ID) error {
			return nil
		},
		LastAcceptedF: snowmantest.MakeLastAcceptedBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
			chain,
		),
	}
	vm.Default(true)
	config.VM = vm

	engine, err := New(config)
	require.NoError(err)
	require.NoError(engine.Start(t.Context(), 0))

	var pollRequestIDs []uint32
	sender.SendPullQueryF = func(_ context.Context, polledNodeIDs set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		require.Equal(set.Of(nodeID), polledNodeIDs)
		pollRequestIDs = append(pollRequestIDs, requestID)
	}

	getRequestIDs := make(map[ids.ID]uint32)
	sender.SendGetF = func(_ context.Context, requestedNodeID ids.NodeID, requestID uint32, blkID ids.ID) {
		require.Equal(nodeID, requestedNodeID)
		getRequestIDs[blkID] = requestID
	}

	// Issue block 0 to trigger poll 0.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID,
		0,
		chain[0].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 1)
	require.Empty(getRequestIDs)

	// Update GetBlock to return, the newly issued, block 0. This is needed to
	// enable the issuance of block 1.
	vm.GetBlockF = MakeGetBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
		chain[:1],
	)

	// Vote for block 2 in poll 0. Under the "vote only on the block at the
	// requested height" semantics, the voter has a single dependency: block 2.
	// Block 2 isn't available yet, so the engine requests it.
	require.NoError(engine.Chits(
		t.Context(),
		nodeID,
		pollRequestIDs[0],
		chain[2].ID(),
		chain[2].ID(),
		snowmantest.GenesisID,
		0,
	))
	require.Len(pollRequestIDs, 1)
	require.Contains(getRequestIDs, chain[2].ID())

	// Deliver block 2. It can't be issued into consensus yet because its parent
	// (block 1) is still missing, so block 2 is only queued as pending and block
	// 1 is requested. The voter is still blocked on block 2 being issued, so
	// poll 0 must NOT be applied yet. This is the early-termination regression:
	// a partially-assembled branch must not terminate the voter.
	require.NoError(engine.Put(
		t.Context(),
		nodeID,
		getRequestIDs[chain[2].ID()],
		chain[2].Bytes(),
	))
	require.Len(pollRequestIDs, 1)
	require.Contains(getRequestIDs, chain[1].ID())
	// Block 2 is only pending issuance, so its status is still Undecided.
	require.Equal(snowtest.Undecided, chain[2].Status)

	// Deliver block 1. This lets block 1 and then block 2 be issued into
	// consensus, which finally resolves the voter's dependency and applies the
	// poll to the whole branch. Each newly issued block extends the preferred
	// chain, so each one also fires a follow-up query (polls for block 1 and
	// block 2).
	require.NoError(engine.Put(
		t.Context(),
		nodeID,
		getRequestIDs[chain[1].ID()],
		chain[1].Bytes(),
	))
	require.Len(pollRequestIDs, 3)
	require.Equal(snowtest.Accepted, chain[0].Status)
	require.Equal(snowtest.Accepted, chain[1].Status)
	require.Equal(snowtest.Accepted, chain[2].Status)
}

// TestEngineForkNearAcceptFrontierAdvancesAcceptFrontier is a liveness regression
// test that tests that a node can still agree on blocks if the votes need to go through
// blocks that fail verification while their parents aren't accepted.
// It runs the same scenario twice: once with small blocks, and once with every block
// inflated past the unverifiedBlockCache size (proving convergence doesn't depend
// on that cache).
//
// The concern is that the votes that a node collects on a losing fork may not bubble to the accept frontier,
// preventing the node from advancing its accept frontier and causing liveness to fail.
//
//	         accept frontier
//	              │
//	Genesis ──────┤
//	 (0)          ├── A0 ── A1 ── A2      forkA: fetched from the remote peer; becomes
//	              │   (1)   (2)   (3)            the node's tall processing chain
//	              │
//	              └── B0 ── B1 ── B2      forkB: what the remote peer prefers;
//	                  (1)   (2)   (3)            diverges from the accepted chain
//	                                             at the accept frontier (Genesis)
//
// The node holds neither fork: it fetches both from the remote peer over the wire.
// It first learns of forkA via a PullQuery for its tip and fetches the whole chain,
// building a tall processing chain. Meanwhile the remote peer keeps voting forkB.
//
// To make the concern as sharp as possible, forkB is verify-gated on parent
// acceptance: B1 cannot be verified until B0 is accepted, and B2 not until B1
// is accepted. So even though the peer's vote points the node at the forkB tip
// (B2), the node cannot verify B2 - it can only make progress by accepting
// forkB from the bottom up. The node must therefore reject its tall forkA and
// climb forkB one accepted block at a time. The test asserts it does exactly
// that and fully converges (liveness holds).
func TestEngineForkNearAcceptFrontierAdvancesAcceptFrontier(t *testing.T) {
	const forkLen = 3
	tests := []struct {
		name      string
		blockSize int
	}{
		{
			name:      "normal blocks",
			blockSize: 0,
		},
		{
			// Every block is inflated, so it overflows the unverifiedBlockCache.
			// This proves vote bubbling does not depend on the byte-bounded
			// cache.
			name:      "blocks larger than cache",
			blockSize: nonVerifiedCacheSize * 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			blockSize := test.blockSize

			forkA := snowmantest.BuildDescendants(snowmantest.Genesis, forkLen)
			forkB := snowmantest.BuildDescendants(snowmantest.Genesis, forkLen)

			// Inflate every block past blockSize, keeping each block's bytes unique (its
			// first bytes are its ID). Must run before the VMs index the blocks by bytes.
			if blockSize > 0 {
				for _, blk := range append(slices.Clone(forkA), forkB...) {
					big := make([]byte, blockSize)
					id := blk.ID()
					copy(big, id[:])
					blk.BytesV = big
				}
			}

			forkATip := forkA[forkLen-1]
			forkBTip := forkB[forkLen-1]

			nodeID := ids.GenerateTestNodeID()
			peerID := ids.GenerateTestNodeID()
			network := snowmanenginetest.NewNetwork(t)

			// [connect] controls whether the engine treats [peer] as a connected validator.
			// A node that hasn't connected its validator can still fetch blocks via Get
			// but its own queries abort for insufficient connected
			// stake - which we use to fetch forkA without triggering forkB convergence.
			newEngine := func(self, peer ids.NodeID, vm *snowmanenginetest.VM, connect bool) *Engine {
				cfg := DefaultConfig(t) // K=1, AlphaPreference=1, AlphaConfidence=1, Beta=1
				require.NoError(cfg.Validators.AddStaker(cfg.Ctx.SubnetID, peer, nil, ids.Empty, 1))
				if connect {
					require.NoError(cfg.ConnectedValidators.Connected(t.Context(), peer, version.Current))
				}
				cfg.Validators.RegisterSetCallbackListener(cfg.Ctx.SubnetID, cfg.ConnectedValidators)
				cfg.VM = vm

				cfg.Sender = network.CreateSender(self)
				snowGetHandler, err := getter.New(vm, cfg.Sender, cfg.Ctx.Log, time.Second, 2000, cfg.Ctx.Registerer)
				require.NoError(err)
				cfg.AllGetsServer = snowGetHandler

				e, err := New(cfg)
				require.NoError(err)
				network.Register(self, e)
				return e
			}

			// The peer holds both forks and serves either on request.
			// It reports the forkB tip as its last accepted block and prefers
			// it, but it also serves forkA blocks over the wire so the node can fetch
			// them.
			peerVM := snowmanenginetest.NewVM(t, append(slices.Clone(forkA), forkB...))
			peerVM.LastAcceptedBlock = forkBTip

			// The node holds neither fork locally: it must fetch every block above Genesis
			// over the wire. It can parse either fork. forkB is verify-gated on parent
			// acceptance, so the node can only verify a forkB block after its parent has
			// been accepted; forkA is not gated.
			forkBIDs := set.Set[ids.ID]{}
			for _, blk := range forkB {
				forkBIDs.Add(blk.ID())
			}
			nodeVM := snowmanenginetest.NewVM(t, append(slices.Clone(forkA), forkB...))
			nodeVM.LastAcceptedBlock = snowmantest.Genesis
			nodeVM.Has = func(*snowmantest.Block) bool {
				return false
			}
			nodeVM.RequireAcceptedParentToVerify = func(blk *snowmantest.Block) bool {
				return forkBIDs.Contains(blk.ID())
			}

			// node is the engine under test, and peer is the remote peer it fetches blocks from.
			// The peer is connected, the node isn't connected so it can't issue queries, just fetch blocks.
			peerEngine := newEngine(peerID, nodeID, peerVM, true)
			nodeEngine := newEngine(nodeID, peerID, nodeVM, false)

			require.NoError(peerEngine.Start(t.Context(), 0))
			require.NoError(nodeEngine.Start(t.Context(), 0))

			// Phase 1 - fetch forkA over the wire. The node learns of forkA through a
			// regular PullQuery for its tip (which carries just the block ID). Since the
			// node holds no forkA blocks, it fetches the whole chain from the peer via
			// Get/Put. Its own queries abort (peer not connected yet), so it
			// builds forkA without any forkB convergence interfering.
			require.NoError(nodeEngine.PullQuery(t.Context(), peerID, 0, forkATip.ID(), 0))
			require.NoError(network.DeliverMessages())
			require.False(network.HasQueuedMessagesToDispatch())

			// The node now holds all of forkA as a tall processing chain preferred over
			// Genesis, with nothing accepted yet.
			for i, blk := range forkA {
				require.Truef(nodeEngine.Consensus.Processing(blk.ID()), "forkA block at height %d not processing", i+1)
			}
			preferenceID, preferenceHeight := nodeEngine.Consensus.Preference()
			require.Equal(forkATip.ID(), preferenceID)
			require.Equal(uint64(forkLen), preferenceHeight)
			nodeLastAcceptedID, nodeLastAcceptedHeight := nodeEngine.Consensus.LastAccepted()
			require.Equal(snowmantest.GenesisID, nodeLastAcceptedID)
			require.Zero(nodeLastAcceptedHeight)

			// The node knows nothing about forkB: with its validator unconnected it never
			// queried the peer, so it never received forkB's IDs and never requested,
			// cached, or issued any forkB block. Only the forkA blocks are processing.
			require.Equal(forkLen, nodeEngine.Consensus.NumProcessing())
			for i, blk := range forkB {
				id := blk.ID()
				require.Falsef(nodeEngine.Consensus.Processing(id), "forkB block at height %d unexpectedly processing", i+1)
				_, pending := nodeEngine.pending[id]
				require.Falsef(pending, "forkB block at height %d unexpectedly pending", i+1)
				_, cached := nodeEngine.unverifiedBlockCache.Get(id)
				require.Falsef(cached, "forkB block at height %d unexpectedly cached", i+1)
				require.Falsef(nodeEngine.blkReqs.HasValue(id), "forkB block at height %d unexpectedly requested", i+1)
			}

			// Phase 2 - converge onto forkB. Connect the peer so the node can query it.
			require.NoError(nodeEngine.ConnectedValidators.Connected(t.Context(), peerID, version.Current))

			// Round 1: the node queries at preferredHeight+1, the peer answers with forkB,
			// and the node fetches forkB. But the blocks of forkB can only be verified upon parent acceptance,
			// so only B0 (whose parent Genesis is accepted) can be verified. The vote for
			// the forkB tip has to bubble to B0 and in each round the previously accepted block unblocks the next one,
			// so the node climbs forkB one block at a time.
			for height := uint64(1); height <= uint64(forkLen); height++ {
				require.NoError(nodeEngine.Gossip(t.Context()))
				require.NoError(network.DeliverMessages())
				require.False(network.HasQueuedMessagesToDispatch())

				_, acceptedHeight := nodeEngine.Consensus.LastAccepted()
				require.Equal(height, acceptedHeight)
			}

			// The accept frontier advanced from Genesis all the way onto forkB.
			acceptedID, acceptedHeight := nodeEngine.Consensus.LastAccepted()
			require.Equal(forkBTip.ID(), acceptedID)
			require.Equal(uint64(forkLen), acceptedHeight)
			require.Zero(nodeEngine.Consensus.NumProcessing())
			for i, blk := range forkB {
				require.Equalf(snowtest.Accepted, blk.Status, "forkB block at height %d not accepted", i+1)
			}
			for i, blk := range forkA {
				require.Equalf(snowtest.Rejected, blk.Status, "forkA block at height %d not rejected", i+1)
			}
		})
	}
}

// Voting for an unissued cached block that fails verification should not
// register any dependencies.
//
// Full blockchain structure:
//
//	  Genesis
//	 /       \
//	0         2
//	|         |
//	1         3
//
// We first issue block 2, and then block 3 fails verification. This causes
// block 3 to be added to the invalid blocks cache.
//
// We then issue block 0, issue block 1, and accept block 0.
//
// If we then vote for block 3, the vote should be dropped and trigger a repoll
// which could then be used to accept block 1.
func TestEngineRegistersInvalidVoterDependencyRegression(t *testing.T) {
	require := require.New(t)

	config := DefaultConfig(t)
	nodeID := ids.GenerateTestNodeID()
	require.NoError(config.Validators.AddStaker(config.Ctx.SubnetID, nodeID, nil, ids.Empty, 1))

	sender := &enginetest.Sender{
		T:          t,
		SendChitsF: func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID, uint64) {},
	}
	sender.Default(true)
	config.Sender = sender

	var (
		acceptedChain = snowmantest.BuildDescendants(snowmantest.Genesis, 2)
		rejectedChain = snowmantest.BuildDescendants(snowmantest.Genesis, 2)
	)
	rejectedChain[1].VerifyV = errInvalid

	vm := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(
				context.Context,
				*snow.Context,
				database.Database,
				[]byte,
				[]byte,
				[]byte,
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
		),
		SetPreferenceF: func(context.Context, ids.ID) error {
			return nil
		},
		LastAcceptedF: snowmantest.MakeLastAcceptedBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
			acceptedChain,
			rejectedChain,
		),
	}
	vm.Default(true)
	config.VM = vm

	engine, err := New(config)
	require.NoError(err)
	require.NoError(engine.Start(t.Context(), 0))

	var pollRequestIDs []uint32
	sender.SendPullQueryF = func(_ context.Context, polledNodeIDs set.Set[ids.NodeID], requestID uint32, _ ids.ID, _ uint64) {
		require.Equal(set.Of(nodeID), polledNodeIDs)
		pollRequestIDs = append(pollRequestIDs, requestID)
	}

	// Issue rejectedChain[0] to consensus.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID,
		0,
		rejectedChain[0].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 1)

	// In order to attempt to issue rejectedChain[1], the engine expects the VM
	// to be willing to provide rejectedChain[0].
	vm.GetBlockF = MakeGetBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
		rejectedChain[:1],
	)

	// Attempt to issue rejectedChain[1] which should add it to the invalid
	// block cache.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID,
		0,
		rejectedChain[1].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 1)

	_, wasCached := engine.unverifiedBlockCache.Get(rejectedChain[1].ID())
	require.True(wasCached)

	// Issue acceptedChain[0] to consensus.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID,
		0,
		acceptedChain[0].Bytes(),
		0,
	))
	// Because acceptedChain[0] isn't initially preferred, a new poll won't be
	// created.
	require.Len(pollRequestIDs, 1)

	// In order to vote for acceptedChain[0], the engine expects the VM to be
	// willing to provide it.
	vm.GetBlockF = MakeGetBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
		acceptedChain[:1],
		rejectedChain[:1],
	)

	// Accept acceptedChain[0] and reject rejectedChain[0].
	require.NoError(engine.Chits(
		t.Context(),
		nodeID,
		pollRequestIDs[0],
		acceptedChain[0].ID(),
		acceptedChain[0].ID(),
		snowmantest.GenesisID,
		0,
	))
	// There are no processing blocks, so no new poll should be created.
	require.Len(pollRequestIDs, 1)
	require.Equal(snowtest.Accepted, acceptedChain[0].Status)
	require.Equal(snowtest.Rejected, rejectedChain[0].Status)

	// Issue acceptedChain[1] to consensus.
	require.NoError(engine.PushQuery(
		t.Context(),
		nodeID,
		0,
		acceptedChain[1].Bytes(),
		0,
	))
	require.Len(pollRequestIDs, 2)

	// Vote for the transitively rejected rejectedChain[1]. This should cause a
	// repoll.
	require.NoError(engine.Chits(
		t.Context(),
		nodeID,
		pollRequestIDs[1],
		rejectedChain[1].ID(),
		rejectedChain[1].ID(),
		snowmantest.GenesisID,
		0,
	))
	require.Len(pollRequestIDs, 3)

	// In order to vote for acceptedChain[1], the engine expects the VM to be
	// willing to provide it.
	vm.GetBlockF = MakeGetBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
		acceptedChain,
		rejectedChain[:1],
	)

	// Accept acceptedChain[1].
	require.NoError(engine.Chits(
		t.Context(),
		nodeID,
		pollRequestIDs[2],
		acceptedChain[1].ID(),
		acceptedChain[1].ID(),
		snowmantest.GenesisID,
		0,
	))
	require.Len(pollRequestIDs, 3)
	require.Equal(snowtest.Accepted, acceptedChain[1].Status)
}

func TestGetProcessingAncestor(t *testing.T) {
	var (
		issuedBlock   = snowmantest.BuildChild(snowmantest.Genesis)
		unissuedBlock = snowmantest.BuildChild(issuedBlock)

		emptyNonVerifiedTree             = ancestor.NewTree()
		nonVerifiedTreeWithUnissuedBlock = ancestor.NewTree()

		emptyNonVerifiedCache             = &cache.Empty[ids.ID, snowman.Block]{}
		nonVerifiedCacheWithUnissuedBlock = lru.NewCache[ids.ID, snowman.Block](1)
		nonVerifiedCacheWithDecidedBlock  = lru.NewCache[ids.ID, snowman.Block](1)
	)
	nonVerifiedTreeWithUnissuedBlock.Add(unissuedBlock.ID(), unissuedBlock.Parent())
	nonVerifiedCacheWithUnissuedBlock.Put(unissuedBlock.ID(), unissuedBlock)
	nonVerifiedCacheWithDecidedBlock.Put(snowmantest.GenesisID, snowmantest.Genesis)

	tests := []struct {
		name             string
		nonVerifieds     ancestor.Tree
		nonVerifiedCache cache.Cacher[ids.ID, snowman.Block]
		initialVote      ids.ID
		expectedAncestor ids.ID
		expectedFound    bool
	}{
		{
			name:             "drop accepted blockID",
			nonVerifieds:     emptyNonVerifiedTree,
			nonVerifiedCache: emptyNonVerifiedCache,
			initialVote:      snowmantest.GenesisID,
			expectedAncestor: ids.Empty,
			expectedFound:    false,
		},
		{
			name:             "drop cached and accepted blockID",
			nonVerifieds:     emptyNonVerifiedTree,
			nonVerifiedCache: nonVerifiedCacheWithDecidedBlock,
			initialVote:      snowmantest.GenesisID,
			expectedAncestor: ids.Empty,
			expectedFound:    false,
		},
		{
			name:             "return processing blockID",
			nonVerifieds:     emptyNonVerifiedTree,
			nonVerifiedCache: emptyNonVerifiedCache,
			initialVote:      issuedBlock.ID(),
			expectedAncestor: issuedBlock.ID(),
			expectedFound:    true,
		},
		{
			name:             "drop unknown blockID",
			nonVerifieds:     emptyNonVerifiedTree,
			nonVerifiedCache: emptyNonVerifiedCache,
			initialVote:      ids.GenerateTestID(),
			expectedAncestor: ids.Empty,
			expectedFound:    false,
		},
		{
			name:             "apply vote through ancestor tree",
			nonVerifieds:     nonVerifiedTreeWithUnissuedBlock,
			nonVerifiedCache: emptyNonVerifiedCache,
			initialVote:      unissuedBlock.ID(),
			expectedAncestor: issuedBlock.ID(),
			expectedFound:    true,
		},
		{
			name:             "apply vote through pending set",
			nonVerifieds:     emptyNonVerifiedTree,
			nonVerifiedCache: nonVerifiedCacheWithUnissuedBlock,
			initialVote:      unissuedBlock.ID(),
			expectedAncestor: issuedBlock.ID(),
			expectedFound:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			var (
				ctx = snowtest.ConsensusContext(
					snowtest.Context(t, snowtest.PChainID),
				)
				consensus = &snowman.Topological{Factory: snowball.SnowflakeFactory}
			)
			require.NoError(consensus.Initialize(
				ctx,
				snowball.DefaultParameters,
				snowmantest.GenesisID,
				0,
				time.Now(),
			))

			require.NoError(consensus.Add(issuedBlock))

			metrics, err := newMetrics(prometheus.NewRegistry())
			require.NoError(err)

			engine := &Engine{
				Config: Config{
					Ctx:       ctx,
					Consensus: consensus,
				},
				metrics:                metrics,
				unverifiedIDToAncestor: test.nonVerifieds,
				unverifiedBlockCache:   test.nonVerifiedCache,
			}

			ancestor, found := engine.getProcessingAncestor(test.initialVote)
			require.Equal(test.expectedAncestor, ancestor)
			require.Equal(test.expectedFound, found)
		})
	}
}

// Test the engine's classification for blocks to either be dropped or try
// issuance.
//
// Full blockchain structure:
//
//	    Genesis
//	   /       \
//	  0         7
//	 / \        |
//	1   4       8
//	|   |      / \
//	2   5     9  11
//	|   |     |
//	3   6     10
//
// Genesis and 0 are accepted.
// 1 is issued.
// 5 and 9 are pending.
//
// Structure known to engine:
//
//	    Genesis
//	   /
//	  0
//	 /
//	1
//
//	    5     9
func TestShouldIssueBlock(t *testing.T) {
	var (
		ctx = snowtest.ConsensusContext(
			snowtest.Context(t, snowtest.PChainID),
		)
		chain0Through3   = snowmantest.BuildDescendants(snowmantest.Genesis, 4)
		chain4Through6   = snowmantest.BuildDescendants(chain0Through3[0], 3)
		chain7Through10  = snowmantest.BuildDescendants(snowmantest.Genesis, 4)
		chain11Through11 = snowmantest.BuildDescendants(chain7Through10[1], 1)
		blocks           = slices.Concat(chain0Through3, chain4Through6, chain7Through10, chain11Through11)
	)

	require.NoError(t, blocks[0].Accept(t.Context()))

	c := &snowman.Topological{Factory: snowball.SnowflakeFactory}
	require.NoError(t, c.Initialize(
		ctx,
		snowball.DefaultParameters,
		blocks[0].ID(),
		blocks[0].Height(),
		blocks[0].Timestamp(),
	))
	require.NoError(t, c.Add(blocks[1]))

	engine := &Engine{
		Config: Config{
			Consensus: c,
		},
		pending: map[ids.ID]snowman.Block{
			blocks[5].ID(): blocks[5],
			blocks[9].ID(): blocks[9],
		},
	}

	tests := []struct {
		name                string
		block               snowman.Block
		expectedShouldIssue bool
	}{
		{
			name:                "genesis",
			block:               snowmantest.Genesis,
			expectedShouldIssue: false,
		},
		{
			name:                "last accepted",
			block:               blocks[0],
			expectedShouldIssue: false,
		},
		{
			name:                "already processing",
			block:               blocks[1],
			expectedShouldIssue: false,
		},
		{
			name:                "next block to enqueue for issuance on top of a processing block",
			block:               blocks[2],
			expectedShouldIssue: true,
		},
		{
			name:                "block to enqueue for issuance which depends on another block",
			block:               blocks[3],
			expectedShouldIssue: true,
		},
		{
			name:                "next block to enqueue for issuance on top of an accepted block",
			block:               blocks[4],
			expectedShouldIssue: true,
		},
		{
			name:                "already pending block",
			block:               blocks[5],
			expectedShouldIssue: false,
		},
		{
			name:                "block to enqueue on top of a pending block",
			block:               blocks[6],
			expectedShouldIssue: true,
		},
		{
			name:                "block was directly rejected",
			block:               blocks[7],
			expectedShouldIssue: false,
		},
		{
			name:                "block was transitively rejected",
			block:               blocks[8],
			expectedShouldIssue: false,
		},
		{
			name:                "block was transitively rejected but that is not known and was marked as pending",
			block:               blocks[9],
			expectedShouldIssue: false,
		},
		{
			name:                "block was transitively rejected but that is not known and is built on top of pending",
			block:               blocks[10],
			expectedShouldIssue: true,
		},
		{
			name:                "block was transitively rejected but that is not known",
			block:               blocks[11],
			expectedShouldIssue: true,
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%d %s", i-1, test.name), func(t *testing.T) {
			shouldIssue := engine.shouldIssueBlock(test.block)
			require.Equal(t, test.expectedShouldIssue, shouldIssue)
		})
	}
}

type mockConnVDR struct {
	tracker.Peers
	percent float64
}

func (m *mockConnVDR) ConnectedPercent() float64 {
	return m.percent
}

type logBuffer struct {
	bytes.Buffer
}

func (logBuffer) Close() error {
	return nil
}

func TestEngineAbortQueryWhenInPartition(t *testing.T) {
	require := require.New(t)

	// Buffer to record the log entries
	buff := logBuffer{}

	conf := DefaultConfig(t)
	// Overwrite the log to record what it says
	conf.Ctx.Log = logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))
	conf.Params = snowball.DefaultParameters
	conf.ConnectedValidators = &mockConnVDR{percent: 0.7, Peers: conf.ConnectedValidators}

	_, _, _, _, engine := setup(t, conf)

	// Gossip will cause a pull query if enough stake is connected
	engine.sendQuery(t.Context(), ids.ID{}, nil, false)

	require.Contains(buff.String(), errInsufficientStake)
}

func TestEngineAcceptedHeight(t *testing.T) {
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

	_, _, _, vm, te := setup(t, engCfg)

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()

	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr0, nil, ids.Empty, 1))
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr1, nil, ids.Empty, 1))

	chain := snowmantest.BuildDescendants(snowmantest.Genesis, 2)

	blk1, blk2 := chain[0], chain[1]

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk1.ID(), nil
	}

	vm.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		return blk1, nil
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	params := snowball.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(engCfg.Consensus.Initialize(ctx, params, blk1.ID(), blk1.Height(), time.Now()))

	require.NoError(te.Chits(t.Context(), vdr0, 1, blk1.ID(), blk1.ID(), blk1.ID(), blk1.Height()))
	require.NoError(te.Chits(t.Context(), vdr1, 2, blk2.ID(), blk2.ID(), blk2.ID(), blk2.Height()))

	eBlk1, h1, ok := te.acceptedFrontiers.LastAccepted(vdr0)
	require.True(ok)

	eBlk2, h2, ok := te.acceptedFrontiers.LastAccepted(vdr1)
	require.True(ok)

	require.Equal(blk1.ID(), eBlk1)
	require.Equal(blk2.ID(), eBlk2)
	require.Equal(blk1.Height(), h1)
	require.Equal(blk2.Height(), h2)
}

func TestPChainProgressUpdaterCalledOnAccept(t *testing.T) {
	require := require.New(t)

	engCfg := DefaultConfig(t)

	var updatedHeight uint64
	var progressUpdated bool
	engCfg.PChainProgressUpdater = &mockPChainProgressUpdater{
		setProgressF: func(height uint64) {
			progressUpdated = true
			updatedHeight = height
		},
	}

	vdr, _, sender, vm, te := setup(t, engCfg)

	sender.Default(true)

	blk := snowmantest.BuildChild(snowmantest.Genesis)

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

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte, _ uint64) {
		*queryRequestID = requestID
	}

	vm.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return blk, nil
	}
	require.NoError(te.Notify(t.Context(), common.PendingTxs))

	require.False(progressUpdated)

	// Vote for the block to accept it
	require.NoError(te.Chits(t.Context(), vdr, *queryRequestID, blk.ID(), blk.ID(), blk.ID(), blk.Height()))

	require.Equal(snowtest.Accepted, blk.Status)
	require.True(progressUpdated)
	require.Equal(blk.Height(), updatedHeight)
}

type mockPChainProgressUpdater struct {
	setProgressF func(height uint64)
}

func (m *mockPChainProgressUpdater) SetProgress(height uint64) {
	m.setProgressF(height)
}

func TestEngineLaggingCatchUpLongGap(t *testing.T) {
	// In this test we have two nodes - a lagging node and an up-to-date node. The
	// lagging node is behind the up-to-date node by 200,000 blocks, and the
	// up-to-date node only answers queries for the top and bottom ranges of blocks below:
	//
	//	height
	//	200,000 ─┐  tip
	//	         │  blocks that the up-to-date node will answer queries for
	//	100,001 ─┘
	//
	//	100,000 ─┐  for these blocks, the request times out, which causes
	//	 90,001 ─┘  a recursive abandonment of block scheduling from the bottom up,
	//              resulting in a call stack 100,000 deep unless we short-circuit it.
	//	 90,000 ─┐
	//	         │  These blocks are answered by the up-to-date node,
	//	      1 ─┘  resulting in the lagging node climbing from the bottom to 90,000.
	//	      0    genesis, where the lagging node starts
	//
	// Both routes run at once. The lagging node climbs from genesis to
	// 90,000 and accepts every block up to it, while the recursive retrieval of blocks from the
	// tip accumulates a job per block and then collapses because the middle range of blocks are not retrievable.
	// We stop the climb at 90,000 only to show that it reaches an arbitrary but high height.

	const (
		// blockGap is how far ahead the up-to-date engine is.
		blockGap = 200_000
		// heightAtWhichUpdatedNodeRefusesToAnswer is the height that above it the up-to-date engine will answer queries,
		// but below it it will refuse answering, unless the height is below catchUpHeight,
		// which is how far the lagging engine can climb.
		heightAtWhichUpdatedNodeRefusesToAnswer = 100_000
		// catchUpHeight is how many blocks the peer answers from the bottom, which is
		// how far the lagging engine can climb.
		catchUpHeight = 90_000
		maxStack      = 8 << 20
	)

	require := require.New(t)
	// We set the max stack limit to something smaller (8MB) so the test will not
	// require a lot of resources to run.
	prevMaxStack := debug.SetMaxStack(maxStack)
	defer debug.SetMaxStack(prevMaxStack) // restore the previous limit once the test finishes

	// Build a chain from genesis to [blockGap] blocks above it.
	chain := make([]*snowmantest.Block, blockGap)
	parent := snowmantest.Genesis
	for i := range chain {
		chain[i] = snowmantest.BuildChild(parent)
		parent = chain[i]
	}
	tip := chain[len(chain)-1]

	laggingNodeID := ids.GenerateTestNodeID()
	upToDateNodeID := ids.GenerateTestNodeID()
	network := snowmanenginetest.NewNetwork(t)

	newEngine := func(self, peer ids.NodeID, vm *snowmanenginetest.VM) *Engine {
		cfg := DefaultConfig(t)
		require.NoError(cfg.Validators.AddStaker(cfg.Ctx.SubnetID, peer, nil, ids.Empty, 1))
		require.NoError(cfg.ConnectedValidators.Connected(t.Context(), peer, version.Current))
		cfg.Validators.RegisterSetCallbackListener(cfg.Ctx.SubnetID, cfg.ConnectedValidators)
		cfg.VM = vm

		cfg.Sender = network.CreateSender(self)
		snowGetHandler, err := getter.New(vm, cfg.Sender, cfg.Ctx.Log, time.Second, 2000, cfg.Ctx.Registerer)
		require.NoError(err)
		cfg.AllGetsServer = snowGetHandler

		e, err := New(cfg)
		require.NoError(err)
		network.Register(self, e)
		return e
	}

	// To make sure the up-to-date node only answers queries for the top and bottom ranges of blocks,
	// we just override its 'Has' method.
	upToDateVM := snowmanenginetest.NewVM(t, chain)
	upToDateVM.LastAcceptedBlock = tip

	// The updated node only replies if the block is either above [heightAtWhichUpdatedNodeRefusesToAnswer]
	// or below [catchUpHeight].
	upToDateVM.Has = func(blk *snowmantest.Block) bool {
		return blk.Height() >= heightAtWhichUpdatedNodeRefusesToAnswer || blk.Height() <= catchUpHeight
	}

	laggingVM := snowmanenginetest.NewVM(t, chain)
	laggingVM.Has = func(blk *snowmantest.Block) bool {
		return blk.Status == snowtest.Accepted
	}

	upToDateEngine := newEngine(upToDateNodeID, laggingNodeID, upToDateVM)
	laggingEngine := newEngine(laggingNodeID, upToDateNodeID, laggingVM)

	require.NoError(upToDateEngine.Start(t.Context(), 0))
	require.NoError(laggingEngine.Start(t.Context(), 0))

	// Trigger the lagging engine to start catching up by gossiping the tip of the chain.
	// Gossip() makes a Pull Query to the up-to-date engine, which will respond with a Get request for the tip.
	require.NoError(laggingEngine.Gossip(t.Context()))
	require.NoError(network.DeliverMessages())
	require.False(network.HasQueuedMessagesToDispatch())

	// The lagging engine climbed from the bottom, one block at a time to our target height.
	_, lastAcceptedHeight := laggingEngine.Consensus.LastAccepted()
	require.Equal(uint64(catchUpHeight), lastAcceptedHeight)

	// The lagging engine should have accepted all blocks up to catchUpHeight.
	for i := range catchUpHeight {
		require.Equal(snowtest.Accepted, chain[i].Status)
	}
}

func TestEngineNetworkBuildsAndAcceptsBlocks(t *testing.T) {
	require := require.New(t)

	// A shared chain that the building engine hands out one block at a time as it
	// is asked to build. Both nodes hold every block, so a peer can parse a block
	// the moment it is queried about it.
	const numBlocks = 100
	chain := snowmantest.BuildDescendants(snowmantest.Genesis, numBlocks)

	network := snowmanenginetest.NewNetwork(t)

	nodeAID := ids.GenerateTestNodeID()
	nodeBID := ids.GenerateTestNodeID()

	vmA := snowmanenginetest.NewVM(t, chain)
	vmB := snowmanenginetest.NewVM(t, chain)

	// Whichever engine is asked to build returns the next block in the chain. An
	// engine only builds on top of its preferred tip, so the chain order matches
	// the order blocks are accepted. Only one engine builds per round, so a single
	// shared cursor over the chain is enough.
	var built int
	buildNext := func(context.Context) (snowman.Block, error) {
		blk := chain[built]
		built++
		return blk, nil
	}
	vmA.BuildBlockF = buildNext
	vmB.BuildBlockF = buildNext

	newEngine := func(self, peer ids.NodeID, vm *snowmanenginetest.VM) *Engine {
		cfg := DefaultConfig(t)
		require.NoError(cfg.Validators.AddStaker(cfg.Ctx.SubnetID, peer, nil, ids.Empty, 1))
		require.NoError(cfg.ConnectedValidators.Connected(t.Context(), peer, version.Current))
		cfg.Validators.RegisterSetCallbackListener(cfg.Ctx.SubnetID, cfg.ConnectedValidators)
		cfg.VM = vm

		cfg.Sender = network.CreateSender(self)
		snowGetHandler, err := getter.New(vm, cfg.Sender, cfg.Ctx.Log, time.Second, 2000, cfg.Ctx.Registerer)
		require.NoError(err)
		cfg.AllGetsServer = snowGetHandler

		e, err := New(cfg)
		require.NoError(err)
		network.Register(self, e)
		return e
	}

	engineA := newEngine(nodeAID, nodeBID, vmA)
	engineB := newEngine(nodeBID, nodeAID, vmB)

	require.NoError(engineA.Start(t.Context(), 0))
	require.NoError(engineB.Start(t.Context(), 0))

	engines := []*Engine{engineA, engineB}
	for i, blk := range chain {
		builder := engines[i%len(engines)]
		require.NoError(builder.Notify(t.Context(), common.PendingTxs)) // Trigger to build the next block in the chain.
		require.NoError(network.DeliverMessages())
		require.False(network.HasQueuedMessagesToDispatch())

		// The block was accepted, and both engines agree on it as their tip.
		require.Equal(snowtest.Accepted, blk.Status)
		for _, e := range engines {
			acceptedID, acceptedHeight := e.Consensus.LastAccepted()
			require.Equal(blk.ID(), acceptedID)
			require.Equal(uint64(i+1), acceptedHeight)
		}
	}
}

// TestEngineAbandonsBlocksTooFarAhead tests when the engine decides to fetch a missing parent block or abandon it.
// If the parent block is above [maxAllowedBlockHeightDistanceIngestion] in height it should abandon the block and not fetch it.
// If the parent block is at or below [maxAllowedBlockHeightDistanceIngestion] in height, it should fetch it.
// The height arithmetic must also tolerate uint64 overflow: when last accepted is near the top of the uint64
// range, adding the allowed distance overflows, and in that case we still fetch.
func TestEngineAbandonsBlocksTooFarAhead(t *testing.T) {
	tests := []struct {
		name                string
		laggingLastAccepted uint64 // the lagging node's last accepted height
		childHeight         uint64 // the offered block, whose parent is missing
		expectFetch         bool
	}{
		{
			name:                "at the cutoff is fetched",
			laggingLastAccepted: 0,
			childHeight:         maxAllowedBlockHeightDistanceIngestion,
			expectFetch:         true,
		},
		{
			name:                "just past the cutoff is abandoned",
			laggingLastAccepted: 0,
			childHeight:         maxAllowedBlockHeightDistanceIngestion + 1,
			expectFetch:         false,
		},
		{
			name:                "overflow of the cutoff is fetched",
			laggingLastAccepted: math.MaxUint64 - 100,
			childHeight:         math.MaxUint64,
			expectFetch:         true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			newBlock := func(height uint64, parentID ids.ID, status snowtest.Status) *snowmantest.Block {
				id := ids.GenerateTestID()
				return &snowmantest.Block{
					Decidable:  snowtest.Decidable{IDV: id, Status: status},
					ParentV:    parentID,
					HeightV:    height,
					TimestampV: snowmantest.GenesisTimestamp,
					BytesV:     id[:],
				}
			}

			// [chain] is every block that both fake VMs know about.
			// We only need a few blocks, at arbitrary heights, not a continuous chain.
			chain := []*snowmantest.Block{snowmantest.Genesis}

			var laggingLastAccepted *snowmantest.Block
			if test.laggingLastAccepted != 0 { // Used to test the uint64 overflow case.
				laggingLastAccepted = newBlock(test.laggingLastAccepted, snowmantest.GenesisID, snowtest.Accepted)
				chain = append(chain, laggingLastAccepted)
			}

			// [missingParent] is the block the lagging node is missing; [lastBlockOfUpdatedNode] is the
			// block the peer prefers, built on top of [missingParent].
			missingParent := newBlock(test.childHeight-1, snowmantest.GenesisID, snowtest.Undecided)
			lastBlockOfUpdatedNode := newBlock(test.childHeight, missingParent.ID(), snowtest.Undecided)
			chain = append(chain, missingParent, lastBlockOfUpdatedNode)

			network := snowmanenginetest.NewNetwork(t)

			laggingNodeID := ids.GenerateTestNodeID()
			upToDateNodeID := ids.GenerateTestNodeID()

			// The up-to-date node prefers [lastBlockOfUpdatedNode] and serves every block except
			// [missingParent], recording whenever it is asked for [missingParent]. That request
			// is the signal that the lagging node chose to fetch rather than
			// abandon.
			var parentRequested bool
			upToDateVM := snowmanenginetest.NewVM(t, chain)
			upToDateVM.LastAcceptedBlock = lastBlockOfUpdatedNode

			// If Has() is called, it means the lagging node is asking for a block.
			// If the block is [missingParent], we record that it was requested.
			upToDateVM.Has = func(blk *snowmantest.Block) bool {
				if blk.ID() == missingParent.ID() {
					parentRequested = true
					return false
				}
				return true
			}

			// The lagging node holds only blocks it has already accepted, so it
			// must fetch anything above its last accepted block from the peer.
			laggingVM := snowmanenginetest.NewVM(t, chain)
			laggingVM.LastAcceptedBlock = laggingLastAccepted
			laggingVM.Has = func(blk *snowmantest.Block) bool {
				return blk.Status == snowtest.Accepted
			}

			newEngine := func(self, peer ids.NodeID, vm *snowmanenginetest.VM) *Engine {
				cfg := DefaultConfig(t)
				require.NoError(cfg.Validators.AddStaker(cfg.Ctx.SubnetID, peer, nil, ids.Empty, 1))
				require.NoError(cfg.ConnectedValidators.Connected(t.Context(), peer, version.Current))
				cfg.Validators.RegisterSetCallbackListener(cfg.Ctx.SubnetID, cfg.ConnectedValidators)
				cfg.VM = vm

				cfg.Sender = network.CreateSender(self)
				snowGetHandler, err := getter.New(vm, cfg.Sender, cfg.Ctx.Log, time.Second, 2000, cfg.Ctx.Registerer)
				require.NoError(err)
				cfg.AllGetsServer = snowGetHandler

				e, err := New(cfg)
				require.NoError(err)
				network.Register(self, e)
				return e
			}

			upToDateEngine := newEngine(upToDateNodeID, laggingNodeID, upToDateVM)
			laggingEngine := newEngine(laggingNodeID, upToDateNodeID, laggingVM)

			require.NoError(upToDateEngine.Start(t.Context(), 0))
			require.NoError(laggingEngine.Start(t.Context(), 0))

			// The lagging node pull-queries the peer (triggered by gossip), learns about [lastBlockOfUpdatedNode], fetches
			// it, and then either requests [missingParent] or abandons it depending on how
			// far ahead [lastBlockOfUpdatedNode] is.
			require.NoError(laggingEngine.Gossip(t.Context()))
			require.NoError(network.DeliverMessages())
			require.False(network.HasQueuedMessagesToDispatch())

			require.Equal(test.expectFetch, parentRequested)
		})
	}
}
