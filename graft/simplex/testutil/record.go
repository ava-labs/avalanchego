// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"testing"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/stretchr/testify/require"
)

// NewEmptyNotarization creates a new empty notarization
func NewEmptyNotarization(nodes []common.NodeID, round uint64) *common.EmptyNotarization {
	var qc TestQC

	for i, node := range nodes {
		qc.Signatures = append(qc.Signatures, Signature{Signer: NewNodeID(node), Value: []byte{byte(i)}})
	}

	return &common.EmptyNotarization{
		QC: qc,
		Vote: common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{
			Round: round,
		}},
	}
}

func NewNotarization(logger common.Logger, signatureAggregator common.SignatureAggregator, block common.VerifiedBlock, ids []common.NodeID) (common.Notarization, error) {
	votesForCurrentRound := make(map[common.NodeID]*common.Vote)
	for _, id := range ids {
		vote, err := NewTestVote(block, id)
		if err != nil {
			return common.Notarization{}, err
		}

		votesForCurrentRound[id] = vote
	}

	notarization, _, err := common.AssembleNotarization(logger, signatureAggregator, votesForCurrentRound, block.BlockHeader())
	return notarization, err
}

func NewNotarizationRecord(logger common.Logger, signatureAggregator common.SignatureAggregator, block common.VerifiedBlock, ids []common.NodeID) ([]byte, error) {
	votesForCurrentRound := make(map[common.NodeID]*common.Vote)
	for _, id := range ids {
		vote, err := NewTestVote(block, id)
		if err != nil {
			return nil, err
		}

		votesForCurrentRound[id] = vote
	}

	_, notarization, err := common.AssembleNotarization(logger, signatureAggregator, votesForCurrentRound, block.BlockHeader())
	if err != nil {
		return nil, err
	}

	record := common.NewQuorumRecord(notarization.QC.Bytes(), notarization.Vote.Bytes(), common.NotarizationRecordType)
	return record, nil
}

// creates a new finalization
func NewFinalizationRecord(t *testing.T, signatureAggregator common.SignatureAggregator, block common.VerifiedBlock, ids []common.NodeID) (common.Finalization, []byte) {
	finalizations := make([]*common.FinalizeVote, len(ids))
	for i, id := range ids {
		finalizations[i] = NewTestFinalizeVote(t, block, id)
	}

	finalization, verified, err := common.AssembleFinalization(signatureAggregator, finalizations)
	require.NoError(t, err)

	record := common.NewQuorumRecord(verified.QC.Bytes(), verified.Finalization.Bytes(), common.FinalizationRecordType)

	return finalization, record
}

// verifiedQC returns the verified form of qc without checking it, so tests can build verified messages
// out of QCs they made invalid on purpose. It only supports the QCs of this package.
func verifiedQC(t *testing.T, qc common.QuorumCertificate) common.VerifiedQuorumCertificate {
	verified, ok := qc.(common.VerifiedQuorumCertificate)
	require.True(t, ok, "%T is not a verified quorum certificate", qc)
	return verified
}

// MustVerifyFinalization returns the verified form of a finalization created by this package.
func MustVerifyFinalization(t *testing.T, f common.Finalization) common.VerifiedFinalization {
	return common.VerifiedFinalization{Finalization: f.Finalization, QC: verifiedQC(t, f.QC)}
}

// MustVerifyNotarization returns the verified form of a notarization created by this package.
func MustVerifyNotarization(t *testing.T, n common.Notarization) common.VerifiedNotarization {
	return common.VerifiedNotarization{Vote: n.Vote, QC: verifiedQC(t, n.QC)}
}

// MustVerifyEmptyNotarization returns the verified form of an empty notarization created by this package.
// It returns nil for a nil notarization.
func MustVerifyEmptyNotarization(t *testing.T, en *common.EmptyNotarization) *common.VerifiedEmptyNotarization {
	if en == nil {
		return nil
	}
	return &common.VerifiedEmptyNotarization{Vote: en.Vote, QC: verifiedQC(t, en.QC)}
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T {
	return &v
}
