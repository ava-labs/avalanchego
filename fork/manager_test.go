// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

type event struct {
	kind     string
	subnetID ids.ID
	nodeID   ids.NodeID
	weight   uint64
}

// recorder records events and maintains the view a listener would build. It
// also flags inconsistent event sequences: a duplicate "added" for a node
// already present, or a "removed"/"changed" for a node the listener was
// never told about.
type recorder struct {
	lock   sync.Mutex
	events []event
	view   map[ids.ID]map[ids.NodeID]uint64
	errs   []error
}

func newRecorder() *recorder {
	return &recorder{view: make(map[ids.ID]map[ids.NodeID]uint64)}
}

func (r *recorder) record(e event) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.events = append(r.events, e)
	subnet, ok := r.view[e.subnetID]
	if !ok {
		subnet = make(map[ids.NodeID]uint64)
		r.view[e.subnetID] = subnet
	}
	_, present := subnet[e.nodeID]
	switch e.kind {
	case "added":
		if present {
			r.errs = append(r.errs, fmt.Errorf("duplicate added event %+v", e))
		}
		subnet[e.nodeID] = e.weight
	case "changed":
		if !present {
			r.errs = append(r.errs, fmt.Errorf("stale changed event %+v", e))
		}
		subnet[e.nodeID] = e.weight
	case "removed":
		if !present {
			r.errs = append(r.errs, fmt.Errorf("stale removed event %+v", e))
		}
		delete(subnet, e.nodeID)
		if len(subnet) == 0 {
			delete(r.view, e.subnetID)
		}
	}
}

// requireConsistent asserts that the recorder never saw a duplicate "added"
// or a stale "removed"/"changed" event.
func (r *recorder) requireConsistent(t *testing.T) {
	r.lock.Lock()
	defer r.lock.Unlock()

	require.Empty(t, r.errs, "listener saw inconsistent events")
}

func (r *recorder) OnValidatorAdded(subnetID ids.ID, nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, weight uint64) {
	r.record(event{"added", subnetID, nodeID, weight})
}

func (r *recorder) OnValidatorRemoved(subnetID ids.ID, nodeID ids.NodeID, weight uint64) {
	r.record(event{"removed", subnetID, nodeID, weight})
}

func (r *recorder) OnValidatorWeightChanged(subnetID ids.ID, nodeID ids.NodeID, _, newWeight uint64) {
	r.record(event{"changed", subnetID, nodeID, newWeight})
}

// setRecorder adapts a recorder to a single subnet's SetCallbackListener.
type setRecorder struct {
	*recorder
	subnetID ids.ID
}

func (s setRecorder) OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	s.recorder.OnValidatorAdded(s.subnetID, nodeID, pk, txID, weight)
}

func (s setRecorder) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	s.recorder.OnValidatorRemoved(s.subnetID, nodeID, weight)
}

func (s setRecorder) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	s.recorder.OnValidatorWeightChanged(s.subnetID, nodeID, oldWeight, newWeight)
}

func forkView(cfg *fork.Config) map[ids.ID]map[ids.NodeID]uint64 {
	primary := make(map[ids.NodeID]uint64)
	for _, v := range cfg.Validators {
		primary[v.NodeID] = v.Weight
	}
	return map[ids.ID]map[ids.NodeID]uint64{constants.PrimaryNetworkID: primary}
}

func TestManagerForwardsBeforeSwitch(t *testing.T) {
	forkNode := ids.GenerateTestNodeID()
	cfg := forktest.NewConfig(t, testForkTime, forkNode)
	m, err := fork.NewManager(validators.NewManager(), cfg, false)
	require.NoError(t, err, "NewManager()")

	r := newRecorder()
	m.RegisterCallbackListener(r)

	srcNode := ids.GenerateTestNodeID()
	require.NoError(t, m.AddStaker(constants.PrimaryNetworkID, srcNode, nil, ids.GenerateTestID(), 5), "AddStaker()")
	require.NoError(t, m.AddWeight(constants.PrimaryNetworkID, srcNode, 2), "AddWeight()")

	require.Equal(t, []event{
		{"added", constants.PrimaryNetworkID, srcNode, 5},
		{"changed", constants.PrimaryNetworkID, srcNode, 7},
	}, r.events, "events")
	require.Equal(t, uint64(7), m.GetWeight(constants.PrimaryNetworkID, srcNode), "GetWeight(source)")
	require.Zero(t, m.GetWeight(constants.PrimaryNetworkID, forkNode), "GetWeight(fork)")
	require.False(t, m.Switched(), "Switched()")
	r.requireConsistent(t)
}

func TestManagerSwitch(t *testing.T) {
	forkNode := ids.GenerateTestNodeID()
	cfg := forktest.NewConfig(t, testForkTime, forkNode)
	m, err := fork.NewManager(validators.NewManager(), cfg, false)
	require.NoError(t, err, "NewManager()")

	var (
		subnetID = ids.GenerateTestID()
		srcNode  = ids.GenerateTestNodeID()
		subNode  = ids.GenerateTestNodeID()
	)
	require.NoError(t, m.AddStaker(constants.PrimaryNetworkID, srcNode, nil, ids.GenerateTestID(), 5), "AddStaker(primary)")
	require.NoError(t, m.AddStaker(subnetID, subNode, nil, ids.GenerateTestID(), 3), "AddStaker(subnet)")

	all := newRecorder()
	m.RegisterCallbackListener(all)
	primary := newRecorder()
	m.RegisterSetCallbackListener(constants.PrimaryNetworkID, setRecorder{primary, constants.PrimaryNetworkID})

	m.Switch()
	m.Switch() // idempotent

	require.Equal(t, forkView(cfg), all.view, "all-subnet listener view after Switch()")
	require.Equal(t, forkView(cfg), primary.view, "primary set listener view after Switch()")
	all.requireConsistent(t)
	primary.requireConsistent(t)
	require.True(t, m.Switched(), "Switched()")

	require.Zero(t, m.GetWeight(constants.PrimaryNetworkID, srcNode), "GetWeight(source) after switch")
	require.Equal(t, uint64(1), m.GetWeight(constants.PrimaryNetworkID, forkNode), "GetWeight(fork) after switch")
	require.Zero(t, m.NumValidators(subnetID), "NumValidators(subnet) after switch")
	require.Equal(t, 1, m.NumSubnets(), "NumSubnets() after switch")

	// The P-chain keeps writing to the private manager; nothing leaks.
	eventsBefore := len(all.events)
	require.NoError(t, m.AddStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID(), nil, ids.GenerateTestID(), 9), "AddStaker() after switch")
	require.Len(t, all.events, eventsBefore, "events after post-switch AddStaker()")
	require.Equal(t, 1, m.NumValidators(constants.PrimaryNetworkID), "NumValidators(primary) after post-switch AddStaker()")
}

func TestManagerRegisterAfterSwitchReplaysForkSet(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
	m, err := fork.NewManager(validators.NewManager(), cfg, false)
	require.NoError(t, err, "NewManager()")
	require.NoError(t, m.AddStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID(), nil, ids.GenerateTestID(), 5), "AddStaker()")
	m.Switch()

	r := newRecorder()
	m.RegisterCallbackListener(r)
	require.Equal(t, forkView(cfg), r.view, "replayed view")
	r.requireConsistent(t)
}

func TestManagerStartSwitched(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	private := validators.NewManager()
	m, err := fork.NewManager(private, cfg, true)
	require.NoError(t, err, "NewManager()")

	r := newRecorder()
	m.RegisterCallbackListener(r)
	// The P-chain populates the private manager after construction.
	require.NoError(t, private.AddStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID(), nil, ids.GenerateTestID(), 5), "private.AddStaker()")

	require.True(t, m.Switched(), "Switched()")
	require.Equal(t, forkView(cfg), r.view, "view")
	r.requireConsistent(t)
}

func TestManagerSwitchRacesWrites(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	m, err := fork.NewManager(validators.NewManager(), cfg, false)
	require.NoError(t, err, "NewManager()")
	r := newRecorder()
	m.RegisterCallbackListener(r)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = m.AddStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID(), nil, ids.GenerateTestID(), 1)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Switch()
	}()
	wg.Wait()

	require.Equal(t, forkView(cfg), r.view, "view after racing writes and Switch()")
	r.requireConsistent(t)
}

func TestManagerRegisterRacesSwitch(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	m, err := fork.NewManager(validators.NewManager(), cfg, false)
	require.NoError(t, err, "NewManager()")
	for range 20 {
		require.NoError(t, m.AddStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID(), nil, ids.GenerateTestID(), 1), "AddStaker()")
	}

	recorders := make([]*recorder, 8)
	var wg sync.WaitGroup
	for i := range recorders {
		recorders[i] = newRecorder()
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RegisterCallbackListener(recorders[i])
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Switch()
	}()
	wg.Wait()

	for i, r := range recorders {
		require.Equal(t, forkView(cfg), r.view, "listener %d view", i)
		r.requireConsistent(t)
	}
}
