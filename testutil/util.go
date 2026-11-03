// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"crypto/rand"
	"encoding/asn1"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/stretchr/testify/require"
)

// DefaultTestNodeEpochConfig returns a default epoch config for a given node.
func DefaultTestNodeEpochConfig(t *testing.T, nodeID common.NodeID, comm common.Communication, bb common.BlockBuilder) (simplex.EpochConfig, *TestWAL, *InMemStorage) {
	l := MakeLogger(t, int(nodeID[0]))
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

func (t *testQCDeserializer) DeserializeQuorumCertificate(bytes []byte) (common.QuorumCertificate, error) {
	var qc []common.Signature
	rest, err := asn1.Unmarshal(bytes, &qc)
	require.NoError(t.t, err)
	require.Empty(t.t, rest)
	return TestQC(qc), err
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
	return TestQC(signatures), t.Err
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

type TestQC []common.Signature

func (t TestQC) Signers() []common.NodeID {
	res := make([]common.NodeID, 0, len(t))
	for _, sig := range t {
		res = append(res, sig.Signer)
	}
	return res
}

func (t TestQC) Verify([]byte, common.Nodes) error {
	return nil
}

func (t TestQC) Bytes() []byte {
	bytes, err := asn1.Marshal(t)
	if err != nil {
		panic(err)
	}
	return bytes
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
	b := make([]byte, 32)

	// fill with cryptographically secure random data
	_, err := rand.Read(b)
	require.NoError(t, err)

	return common.NodeID(b)
}
