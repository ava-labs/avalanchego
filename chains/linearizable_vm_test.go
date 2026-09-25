// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap/queue"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex/vertextest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/metervm"

	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
)

func TestInitializeOnLinearizeVMLateAncestorsDoesNotReLinearize(t *testing.T) {
	// This test tests a scenario where the stop vertex arrives and the bootstrapper sends
	// two GetAncestors requests for its two unknown parents.
	// The first response arrives and is enough to linearize, and the bootstrapper linearizes, initializing the snowman VM stack.
	// The second response arrives late and must not re-linearize the snowman VM stack.

	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.XChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	// vtx2 is the stop vertex and it has two parents in the DAG, vtx0 and vtx1:
	//    vtx0
	//    / \
	//   /  vtx1
	//   |  /
	//  vtx2

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{IDV: ids.GenerateTestID(), StatusV: choices.Unknown},
		HeightV:       0,
		BytesV:        []byte{0},
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{IDV: ids.GenerateTestID(), StatusV: choices.Unknown},
		ParentsV:      []avalanche.Vertex{vtx0},
		HeightV:       1,
		BytesV:        []byte{1},
	}
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{IDV: ids.GenerateTestID(), StatusV: choices.Processing},
		ParentsV:      []avalanche.Vertex{vtx1, vtx0},
		HeightV:       2,
		BytesV:        []byte{2},
	}
	vtxs := []*avalanche.TestVertex{vtx0, vtx1, vtx2}

	errUnknownVertex := errors.New("unknown vertex")

	manager := vertextest.NewManager(t)
	manager.Default(true)
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
		for _, vtx := range vtxs {
			if vtx.ID() == vtxID && vtx.Status() != choices.Unknown {
				return vtx, nil
			}
		}
		return nil, errUnknownVertex
	}
	manager.ParseVtxF = func(_ context.Context, vtxBytes []byte) (avalanche.Vertex, error) {
		for _, vtx := range vtxs {
			if bytes.Equal(vtx.Bytes(), vtxBytes) {
				if vtx.Status() == choices.Unknown {
					vtx.StatusV = choices.Processing
				}
				return vtx, nil
			}
		}
		return nil, errUnknownVertex
	}
	manager.StopVertexAcceptedF = func(context.Context) (bool, error) {
		return false, nil
	}
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vtx2.ID()}
	}

	// Below we have the snowman VM stack that will be initialized when the bootstrapper linearizes.
	innerVM := &vertextest.VM{}
	innerVM.T = t
	innerVM.Default(true)
	innerVM.CantSetState = false
	innerVM.LinearizeF = func(context.Context, ids.ID) error {
		return nil
	}
	vmToLinearize := NewLinearizeOnInitializeVM(innerVM)
	vm := &initializeOnLinearizeVM{
		DAGVM: innerVM,
		// The metervm BlockVM below will be initialized when the bootstrapper linearizes.
		// If initialized twice it will return an error, which is what the test is checking for.
		vmToInitialize:   metervm.NewBlockVM(vmToLinearize, prometheus.NewRegistry()),
		vmToLinearize:    vmToLinearize,
		ctx:              snowCtx,
		db:               memdb.New(),
		waitForLinearize: make(chan struct{}),
	}

	// The bootstrapper only accepts an Ancestors response whose (nodeID, requestID)
	// matches a request it sent, and it expects the first vertex in the response to
	// be the one that request asked for. Request IDs are assigned in the order vertices
	// are popped from a set, so which ID belongs to which vertex differs between runs.
	var peer ids.NodeID
	requestIDs := make(map[ids.ID]uint32)
	sender := &enginetest.Sender{T: t}
	sender.Default(true)
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, vtxID ids.ID) {
		peer = nodeID
		requestIDs[vtxID] = requestID
	}

	db := memdb.New()
	vtxBlocked, err := queue.NewWithMissing(prefixdb.New([]byte("vtx"), db), "vtx", prometheus.NewRegistry())
	require.NoError(err)
	txBlocked, err := queue.New(prefixdb.New([]byte("tx"), db), "tx", prometheus.NewRegistry())
	require.NoError(err)
	peerTracker, err := p2p.NewPeerTracker(logging.NoLog{}, "", prometheus.NewRegistry(), nil, version.Current)
	require.NoError(err)

	bs, err := avbootstrap.New(
		avbootstrap.Config{
			Ctx:                            ctx,
			Sender:                         sender,
			PeerTracker:                    peerTracker,
			AncestorsMaxContainersReceived: 2000,
			VtxBlocked:                     vtxBlocked,
			TxBlocked:                      txBlocked,
			Manager:                        manager,
			VM:                             vm,
			StopVertexID:                   vtx2.ID(),
			Haltable:                       &common.Halter{},
		},
		func(context.Context, uint32) error { return nil },
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	// Both parents of the stop vertex are unknown, so two requests go out.
	require.NoError(bs.Start(t.Context(), 0))
	require.Len(requestIDs, 2)

	// The response for vtx1 also carries vtx0. Nothing is missing afterwards,
	// so the bootstrapper linearizes and the VM stack is initialized.
	require.NoError(bs.Ancestors(t.Context(), peer, requestIDs[vtx1.ID()], [][]byte{vtx1.Bytes(), vtx0.Bytes()}))

	// The response for vtx0 arrives after linearization and should not trigger it again.
	// If it does, the inner meterVM will return an error and the test will fail.
	require.NoError(bs.Ancestors(t.Context(), peer, requestIDs[vtx0.ID()], [][]byte{vtx0.Bytes()}))
}
