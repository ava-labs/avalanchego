// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/stretchr/testify/require"
)

// NewEmptyNotarization creates a new empty notarization
func NewEmptyNotarization(nodes []common.NodeID, round uint64) *common.EmptyNotarization {
	var qc TestQC

	for i, node := range nodes {
		qc = append(qc, common.Signature{Signer: node, Value: []byte{byte(i)}})
	}

	return &common.EmptyNotarization{
		QC: qc,
		Vote: common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{
			Round: round,
		}},
	}
}

func NewNotarization(logger common.Logger, signatureAggregator common.SignatureAggregator, block common.VerifiedBlock, ids []common.NodeID) (common.Notarization, error) {
	votesForCurrentRound := make(map[string]*common.Vote)
	for _, id := range ids {
		vote, err := NewTestVote(block, id)
		if err != nil {
			return common.Notarization{}, err
		}

		votesForCurrentRound[string(id)] = vote
	}

	notarization, err := common.NewNotarization(logger, signatureAggregator, votesForCurrentRound, block.BlockHeader())
	return notarization, err
}

func NewNotarizationRecord(logger common.Logger, signatureAggregator common.SignatureAggregator, block common.VerifiedBlock, ids []common.NodeID) ([]byte, error) {
	notarization, err := NewNotarization(logger, signatureAggregator, block, ids)
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

	finalization, err := common.NewFinalization(signatureAggregator, finalizations)
	require.NoError(t, err)

	record := common.NewQuorumRecord(finalization.QC.Bytes(), finalization.Finalization.Bytes(), common.FinalizationRecordType)

	return finalization, record
}
