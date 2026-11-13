// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/simplex"
	"github.com/stretchr/testify/require"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto util.go

// DefaultTestNodeEpochConfig returns a default epoch config for a given node.
func DefaultTestNodeEpochConfig(t *testing.T, nodeID common.NodeID, comm common.Communication, bb common.BlockBuilder) (simplex.EpochConfig, *TestWAL, *InMemStorage) {
	l := MakeLogger(t, int(nodeID.ShortID[0]))
	storage := NewInMemStorage()
	wal := NewTestWAL(t)
	conf := simplex.EpochConfig{
		MaxBlacklistedRounds: simplex.DefaultMaxBlacklistedRounds,
		MaxRoundWindow:       simplex.DefaultMaxRoundWindow,
		MaxProposalWait:      simplex.DefaultMaxProposalWaitTime,
		MaxRebroadcastWait:   simplex.DefaultEmptyVoteRebroadcastTimeout,
		Comm:                 comm,
		Logger:               l,
		ID:                   nodeID,
		Signer:               &TestSigner{},
		WAL:                  wal,
		Verifier:             &TestVerifier{},
		Storage:              storage,
		BlockBuilder:         bb,
		SignatureAggregatorCreator: func(weights []common.Node) common.SignatureAggregator {
			return &TestSignatureAggregator{N: len(weights)}
		},
		BlockDeserializer: &BlockDeserializer{},
		QCDeserializer:    &testQCDeserializer{t: t},
		StartTime:         time.Now(),
	}
	return conf, wal, storage
}

type AnyBlock interface {
	// BlockHeader encodes a succinct and collision-free representation of a block.
	BlockHeader() common.BlockHeader
}

func NewTestVote(block AnyBlock, id common.NodeID) (*common.Vote, error) {
	vote := common.ToBeSignedVote{
		BlockHeader: block.BlockHeader(),
	}
	sig, err := vote.Sign(&TestSigner{})
	if err != nil {
		return nil, err
	}

	return &common.Vote{
		Signature: common.Signature{
			Signer: id,
			Value:  sig,
		},
		Vote: vote,
	}, nil
}

func InjectTestVote(t *testing.T, e *simplex.Epoch, block common.VerifiedBlock, id common.NodeID) {
	vote, err := NewTestVote(block, id)
	require.NoError(t, err)
	err = e.HandleMessage(&common.Message{
		VoteMessage: vote,
	}, id)
	require.NoError(t, err)
}

func NewTestFinalizeVote(t *testing.T, block common.VerifiedBlock, id common.NodeID) *common.FinalizeVote {
	f := common.ToBeSignedFinalization{BlockHeader: block.BlockHeader()}
	sig, err := f.Sign(&TestSigner{})
	require.NoError(t, err)
	return &common.FinalizeVote{
		Signature: common.Signature{
			Signer: id,
			Value:  sig,
		},
		Finalization: common.ToBeSignedFinalization{
			BlockHeader: block.BlockHeader(),
		},
	}
}

func InjectTestFinalization(t *testing.T, e *simplex.Epoch, finalization *common.Finalization, from common.NodeID) {
	err := e.HandleMessage(&common.Message{
		Finalization: finalization,
	}, from)
	require.NoError(t, err)
}

func InjectTestFinalizeVote(t *testing.T, e *simplex.Epoch, block common.VerifiedBlock, id common.NodeID) {
	err := e.HandleMessage(&common.Message{
		FinalizeVote: NewTestFinalizeVote(t, block, id),
	}, id)
	require.NoError(t, err)
}

func InjectTestNotarization(t *testing.T, e *simplex.Epoch, notarization common.Notarization, id common.NodeID) {
	err := e.HandleMessage(&common.Message{
		Notarization: &notarization,
	}, id)
	require.NoError(t, err)
}

type testQCDeserializer struct {
	t *testing.T
}

func (t *testQCDeserializer) DeserializeQuorumCertificate(bytes []byte, _ common.NodeIDs) (common.QuorumCertificate, error) {
	qc, err := DeserializeTestQC(bytes)
	require.NoError(t.t, err)
	return qc, nil
}

type TestSignatureAggregator struct {
	Err                         error
	N                           int
	IsQuorumFunc                func(signatures []common.NodeID) bool
	AboveByzantineThresholdFunc func(signatures []common.NodeID) bool
}

func (t *TestSignatureAggregator) AppendSignatures(existing common.SignatureBytes, sigs ...common.SignatureBytes) (common.SignatureBytes, error) {
	if t.Err != nil {
		return nil, t.Err
	}
	result := append([]byte{}, existing...)
	for _, s := range sigs {
		result = append(result, s...)
	}
	return result, nil
}

func (t *TestSignatureAggregator) Aggregate(signatures []common.Signature) (common.QuorumCertificate, error) {
	qc := TestQC{Signatures: make([]Signature, len(signatures))}
	for i, sig := range signatures {
		qc.Signatures[i] = Signature{Signer: NewNodeID(sig.Signer), Value: sig.Value}
	}
	return qc, t.Err
}

func (t *TestSignatureAggregator) IsQuorum(signers []common.NodeID) bool {
	if t.IsQuorumFunc != nil {
		return t.IsQuorumFunc(signers)
	}
	if t.N == 0 {
		panic("uninitialized TestSignatureAggregator")
	}

	return len(signers) >= common.Quorum(t.N)
}

func (t *TestSignatureAggregator) AboveByzantineThreshold(signers []common.NodeID) bool {
	if t.AboveByzantineThresholdFunc != nil {
		return t.AboveByzantineThresholdFunc(signers)
	}
	if t.N == 0 {
		panic("uninitialized TestSignatureAggregator")
	}

	return len(signers) >= common.F(t.N)+1
}

// NodeID is the serializable form of a [common.NodeID].
// It cannot be [metadata.NodeID] because msm tests import this package.
type NodeID struct {
	ShortID [20]byte `canoto:"fixed bytes,1"`
	LongID  [32]byte `canoto:"fixed bytes,2"`

	canotoData canotoData_NodeID
}

// NewNodeID converts a [common.NodeID] to its serializable form.
func NewNodeID(nodeID common.NodeID) NodeID {
	return NodeID{ShortID: nodeID.ShortID, LongID: nodeID.LongID}
}

// ToNodeID converts the serializable form back to a [common.NodeID].
func (nodeID NodeID) ToNodeID() common.NodeID {
	return common.NodeID{ShortID: nodeID.ShortID, LongID: nodeID.LongID}
}

type Signature struct {
	// Signer is the NodeID of the creator of the signature.
	Signer NodeID `canoto:"value,1"`
	// Value is the byte representation of the signature.
	Value common.SignatureBytes `canoto:"bytes,2"`

	canotoData canotoData_Signature
}

type TestQC struct {
	Signatures []Signature `canoto:"repeated value,1"`

	canotoData canotoData_TestQC
}

func (t TestQC) Signers() []common.NodeID {
	res := make([]common.NodeID, 0, len(t.Signatures))
	for _, sig := range t.Signatures {
		res = append(res, sig.Signer.ToNodeID())
	}
	return res
}

func (t TestQC) Verify([]byte, common.Nodes) error {
	return nil
}

func (t TestQC) Bytes() []byte {
	// Marshal a copy so the canoto size caches never end up in t itself,
	// otherwise two equal TestQCs would not be equal in require.Equal once one of them was marshaled.
	qc := t.withoutCache()
	return qc.MarshalCanoto()
}

// withoutCache returns a deep copy of the TestQC whose canoto caches are empty.
func (t TestQC) withoutCache() TestQC {
	if len(t.Signatures) == 0 {
		return TestQC{}
	}
	signatures := make([]Signature, len(t.Signatures))
	for i, sig := range t.Signatures {
		signatures[i] = Signature{
			Signer: NodeID{ShortID: sig.Signer.ShortID, LongID: sig.Signer.LongID},
			Value:  sig.Value,
		}
	}
	return TestQC{Signatures: signatures}
}

// DeserializeTestQC parses bytes produced by [TestQC.Bytes].
func DeserializeTestQC(bytes []byte) (TestQC, error) {
	var qc TestQC
	if err := qc.UnmarshalCanoto(bytes); err != nil {
		return TestQC{}, err
	}
	return qc.withoutCache(), nil
}
func (t TestQC) Size() int {
	return len(t.Bytes())
}

type TestSigner struct {
}

func (t *TestSigner) Sign([]byte) (common.SignatureBytes, error) {
	return common.SignatureBytes{1, 2, 3}, nil
}

type TestVerifier struct {
}

func (t *TestVerifier) VerifyBlock(common.VerifiedBlock) error {
	return nil
}

func (t *TestVerifier) VerifySignature(_ []byte, _ common.SignatureBytes, _ common.PublicKeyBytes) error {
	return nil
}

func WaitToEnterRound(t *testing.T, e *simplex.Epoch, round uint64) {
	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()

	for {
		if e.Metadata().Round >= round {
			return
		}

		select {
		case <-time.After(time.Millisecond * 10):
			continue
		case <-timeout.C:
			require.Fail(t, "timed out waiting to enter round", "current round %d, waiting for round %d", e.Metadata().Round, round)
		}
	}
}

func WaitToEnterRoundWithTimeout(t *testing.T, e *simplex.Epoch, round uint64, timeoutDuration time.Duration) {
	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()

	for {
		if e.Metadata().Round >= round {
			return
		}

		select {
		case <-time.After(time.Millisecond * 10):
			continue
		case <-timeout.C:
			require.Fail(t, "timed out waiting to enter round", "Node ID %s, current round %d, waiting for round %d", e.ID, e.Metadata().Round, round)
		}
	}
}

func WaitForBlockProposerTimeout(t *testing.T, e *simplex.Epoch, startTime *time.Time, startRound uint64) {
	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()

	for {
		if e.WAL.(*TestWAL).ContainsEmptyVote(startRound) || e.WAL.(*TestWAL).ContainsEmptyNotarization(startRound) {
			return
		}

		// if we are expected to time out for this round, we should not have a notarization
		require.False(t, e.WAL.(*TestWAL).ContainsNotarization(startRound), fmt.Sprintf("should not have notarized %d for node %s", startRound, e.ID))

		*startTime = startTime.Add(e.MaxProposalWait / 5)
		e.AdvanceTime(*startTime)
		select {
		case <-time.After(time.Millisecond * 10):
			continue
		case <-timeout.C:
			require.Fail(t, "timed out waiting for event")
		}
	}
}

func GenerateNodeID(t *testing.T) common.NodeID {
	var nodeID common.NodeID

	// fill with cryptographically secure random data
	_, err := rand.Read(nodeID.ShortID[:])
	require.NoError(t, err)

	return nodeID
}

// EqualWeights returns the weights of nodeCount nodes that all have a weight of 1.
func EqualWeights(nodeCount uint16) []uint64 {
	weights := make([]uint64, nodeCount)
	for i := range weights {
		weights[i] = 1
	}
	return weights
}
