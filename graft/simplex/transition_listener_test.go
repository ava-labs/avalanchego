// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/avalanchego/graft/simplex/avalanchego"
	"github.com/ava-labs/avalanchego/graft/simplex/common"
	metadata "github.com/ava-labs/avalanchego/graft/simplex/msm"
	"github.com/ava-labs/avalanchego/graft/simplex/testutil"

	"github.com/stretchr/testify/require"
)

const testEpoch = uint64(5)

var (
	testNodeID    = avalanchego.NodeID{1}
	stubSignature = common.SignatureBytes("signature")
)

type recordingSender struct {
	messages     []*common.Message
	destinations []common.NodeID
}

func (rs *recordingSender) Send(msg *common.Message, destination common.NodeID) {
	rs.messages = append(rs.messages, msg)
	rs.destinations = append(rs.destinations, destination)
}

type stubSigner struct {
	sig []byte
}

func (s stubSigner) Sign([]byte) (common.SignatureBytes, error) {
	return s.sig, nil
}

type stubAuxInfoApp struct {
	sufficient bool
	generated  []byte
}

func (s *stubAuxInfoApp) IsLegalAppend(common.VersionID, metadata.NodeBLSMappings, [][]byte, []byte) error {
	return nil
}

func (s *stubAuxInfoApp) IsSufficient(common.VersionID, metadata.NodeBLSMappings, [][]byte) (bool, error) {
	return s.sufficient, nil
}

func (s *stubAuxInfoApp) Generate(common.VersionID, metadata.NodeBLSMappings, [][]byte) ([]byte, error) {
	return s.generated, nil
}

func (s *stubAuxInfoApp) DefaultVersionID() common.VersionID {
	return 7
}

type listenerTestEnv struct {
	sender *recordingSender
	// approvals records the approvals fed via the handleApproval callback.
	approvals []common.ValidatorSetApproval
	listener  *epochTransitionListener

	// sealingBlock & finalization for the current epoch
	sealingBlock        metadata.StateMachineBlock
	sealingFinalization *common.Finalization
}

// newSealingBlock returns a block whose BlockValidationDescriptor carries the given validator set.
func newSealingBlock(validators metadata.NodeBLSMappings) metadata.StateMachineBlock {
	return metadata.StateMachineBlock{
		Metadata: metadata.StateMachineMetadata{
			SimplexEpochInfo: metadata.SimplexEpochInfo{
				BlockValidationDescriptor: &metadata.BlockValidationDescriptor{
					AggregatedMembership: metadata.AggregatedMembership{Members: validators},
				},
			},
		},
	}
}

// newListenerTestEnv builds a listener wired to the given current and next-epoch validator sets and auxApp.
// The current validator set is served by a finalized sealing block at [testEpoch].
func newListenerTestEnv(t *testing.T, currentValidatorSet metadata.NodeBLSMappings, nextEpochValidatorSet metadata.NodeBLSMappings, auxApp metadata.AuxiliaryInfoGenVerifier) *listenerTestEnv {
	env := &listenerTestEnv{
		sender:              &recordingSender{},
		sealingBlock:        newSealingBlock(currentValidatorSet),
		sealingFinalization: &common.Finalization{},
	}

	getValidatorSet := func(uint64) (metadata.NodeBLSMappings, error) {
		return nextEpochValidatorSet, nil
	}
	getBlock := func(seq uint64, _ common.Digest) (metadata.StateMachineBlock, *common.Finalization, error) {
		require.Equal(t, testEpoch, seq, "unexpected getBlock call")
		return env.sealingBlock, env.sealingFinalization, nil
	}

	handleApproval := func(approval *common.ValidatorSetApproval) {
		env.approvals = append(env.approvals, *approval)
	}

	env.listener = newEpochTransitionListener(
		testutil.MakeLogger(t, 1),
		env.sender,
		testNodeID,
		getValidatorSet,
		getBlock,
		stubSigner{sig: stubSignature},
		auxApp,
		handleApproval,
	)
	return env
}

// newTransitionBlock returns a ParsedBlock of type BlockTypeTransitioning carrying the
// given next-epoch P-chain reference height. The listener supplies the validator set and
// auxiliary info app, so the block itself needs no MSM.
func newTransitionBlock(t *testing.T, nextPChainRef uint64) *ParsedBlock {
	block := &ParsedBlock{
		StateMachineBlock: metadata.StateMachineBlock{
			Metadata: metadata.StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Seq: 10, Epoch: testEpoch},
				SimplexEpochInfo: metadata.SimplexEpochInfo{
					NextPChainReferenceHeight: nextPChainRef,
				},
			},
		},
	}
	require.Equal(t, metadata.BlockTypeTransitioning, block.Type())
	return block
}

func TestTransitionNotInValidatorSet(t *testing.T) {
	// the next validator set does not contain our node
	otherValidator := metadata.NodeBLSMappings{{NodeID: avalanchego.NodeID{2}, Weight: 1}}
	auxApp := &stubAuxInfoApp{sufficient: false, generated: []byte("more aux info")}
	env := newListenerTestEnv(t, otherValidator, otherValidator, auxApp)

	block := newTransitionBlock(t, 100)

	require.NoError(t, env.listener.handleTransitionBlock(block))
	require.Empty(t, env.sender.messages)
	require.Empty(t, env.approvals)
}

// TestTransitionNotEnoughAuxiliary tests that if we do not have a sufficient amount of auxiliary information
// we will send our own to the current validators before sending any approvals.
func TestTransitionNotEnoughAuxiliary(t *testing.T) {
	peer1 := avalanchego.NodeID{2}
	peer2 := avalanchego.NodeID{3}
	leavingPeer := avalanchego.NodeID{4}
	currentValidatorSet := metadata.NodeBLSMappings{{NodeID: testNodeID, Weight: 1}, {NodeID: peer1, Weight: 1}, {NodeID: leavingPeer, Weight: 1}}
	nextValidatorSet := metadata.NodeBLSMappings{{NodeID: testNodeID, Weight: 1}, {NodeID: peer1, Weight: 1}, {NodeID: peer2, Weight: 1}}
	auxApp := &stubAuxInfoApp{sufficient: false, generated: []byte("more aux info")}
	env := newListenerTestEnv(t, currentValidatorSet, nextValidatorSet, auxApp)

	block := newTransitionBlock(t, 100)

	require.NoError(t, env.listener.handleTransitionBlock(block))

	// the generated auxiliary info is sent to every other validator instead of an approval
	require.Len(t, env.sender.messages, 2)
	require.Equal(t, []common.NodeID{common.NodeID(peer1[:]), common.NodeID(leavingPeer[:])}, env.sender.destinations)
	for _, msg := range env.sender.messages {
		require.Nil(t, msg.EpochTransitionApproval)
		require.NotNil(t, msg.AuxiliaryInfo)
		require.Equal(t, testEpoch, msg.AuxiliaryInfo.Epoch)
		require.Equal(t, auxApp.DefaultVersionID(), msg.AuxiliaryInfo.Version)
		require.Equal(t, auxApp.generated, msg.AuxiliaryInfo.Data)
	}
	require.Empty(t, env.approvals)
}

func TestTransitionSendsApproval(t *testing.T) {
	const nextPChainRef = uint64(100)

	peer := avalanchego.NodeID{2}
	leavingPeer := avalanchego.NodeID{3}
	joiningPeer := avalanchego.NodeID{4}
	currentValidatorSet := metadata.NodeBLSMappings{{NodeID: testNodeID, Weight: 1}, {NodeID: peer, Weight: 1}, {NodeID: leavingPeer, Weight: 1}}
	nextValidatorSet := metadata.NodeBLSMappings{{NodeID: testNodeID, Weight: 1}, {NodeID: peer, Weight: 1}, {NodeID: joiningPeer, Weight: 1}}
	env := newListenerTestEnv(t, currentValidatorSet, nextValidatorSet, &stubAuxInfoApp{sufficient: true})

	block := newTransitionBlock(t, nextPChainRef)

	require.NoError(t, env.listener.handleTransitionBlock(block))

	// the approval is sent to the other current validators, not to ourselves or the joining peer
	require.Len(t, env.sender.messages, 2)
	require.Equal(t, []common.NodeID{common.NodeID(peer[:]), common.NodeID(leavingPeer[:])}, env.sender.destinations)
	msg := env.sender.messages[0]
	require.Nil(t, msg.AuxiliaryInfo)
	require.NotNil(t, msg.EpochTransitionApproval)

	approval := msg.EpochTransitionApproval
	require.Equal(t, testNodeID, approval.NodeID)
	require.Equal(t, nextPChainRef, approval.PChainHeight)
	require.Equal(t, common.Digest{}, approval.AuxInfoDigest) // no auxiliary info was collected
	require.Equal(t, stubSignature, approval.Signature)

	// a validator also records its own approval locally so its next block includes it
	require.Equal(t, []common.ValidatorSetApproval{*approval}, env.approvals)
}

// TestNonValidatorContributesAuxiliaryInfo asserts that a node still on the outside of the
// current validator set but present in the NEXT one contributes auxiliary info during the
// transition, exactly like a validator does. Non-validators pass a nil handleApproval, so
// nothing is recorded locally, but the auxiliary info is still sent.
func TestNonValidatorContributesAuxiliaryInfo(t *testing.T) {
	peer1 := avalanchego.NodeID{2}
	peer2 := avalanchego.NodeID{3}
	currentValidatorSet := metadata.NodeBLSMappings{{NodeID: peer1, Weight: 1}, {NodeID: peer2, Weight: 1}}
	nextValidatorSet := metadata.NodeBLSMappings{{NodeID: testNodeID, Weight: 1}, {NodeID: peer1, Weight: 1}}
	auxApp := &stubAuxInfoApp{sufficient: false, generated: []byte("non-validator aux")}
	env := newListenerTestEnv(t, currentValidatorSet, nextValidatorSet, auxApp)

	block := newTransitionBlock(t, 100)

	require.NoError(t, env.listener.handleTransitionBlock(block))

	// the auxiliary info is sent to every current validator
	require.Len(t, env.sender.messages, 2)
	require.Equal(t, []common.NodeID{common.NodeID(peer1[:]), common.NodeID(peer2[:])}, env.sender.destinations)
	for _, msg := range env.sender.messages {
		require.Nil(t, msg.EpochTransitionApproval)
		require.NotNil(t, msg.AuxiliaryInfo)
		require.Equal(t, auxApp.DefaultVersionID(), msg.AuxiliaryInfo.Version)
		require.Equal(t, auxApp.generated, msg.AuxiliaryInfo.Data)
	}
}

// TestNonValidatorContributesApproval asserts that once the auxiliary info history is
// sufficient, a non-validator that belongs to the next validator set sends its
// epoch transition approval.
func TestNonValidatorContributesApproval(t *testing.T) {
	const nextPChainRef = uint64(100)

	peer1 := avalanchego.NodeID{2}
	peer2 := avalanchego.NodeID{3}
	currentValidatorSet := metadata.NodeBLSMappings{{NodeID: peer1, Weight: 1}, {NodeID: peer2, Weight: 1}}
	nextValidatorSet := metadata.NodeBLSMappings{{NodeID: testNodeID, Weight: 1}, {NodeID: peer1, Weight: 1}}
	env := newListenerTestEnv(t, currentValidatorSet, nextValidatorSet, &stubAuxInfoApp{sufficient: true})

	block := newTransitionBlock(t, nextPChainRef)

	require.NoError(t, env.listener.handleTransitionBlock(block))

	// the approval is sent to every current validator
	require.Len(t, env.sender.messages, 2)
	require.Equal(t, []common.NodeID{common.NodeID(peer1[:]), common.NodeID(peer2[:])}, env.sender.destinations)
	msg := env.sender.messages[0]
	require.Nil(t, msg.AuxiliaryInfo)
	require.NotNil(t, msg.EpochTransitionApproval)

	approval := msg.EpochTransitionApproval
	require.Equal(t, testNodeID, approval.NodeID)
	require.Equal(t, nextPChainRef, approval.PChainHeight)
	require.Equal(t, stubSignature, approval.Signature)
}
