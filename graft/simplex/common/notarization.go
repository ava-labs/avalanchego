// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"go.uber.org/zap"
)

var (
	ErrorNoVotes                   = errors.New("no votes to notarize")
	ErrorInvalidFinalizationDigest = errors.New("finalization digests do not match")
)

func NewEmptyNotarizationRecord(emptyNotarization *EmptyNotarization) []byte {
	return NewQuorumRecord(emptyNotarization.QC.Bytes(), emptyNotarization.Vote.Bytes(), EmptyNotarizationRecordType)
}

func EmptyNotarizationFromRecord(record []byte, qd QCDeserializer) (EmptyNotarization, error) {
	qcBytes, emptyVote, err := ParseEmptyNotarizationRecord(record)
	if err != nil {
		return EmptyNotarization{}, err
	}

	qc, err := qd.DeserializeQuorumCertificate(qcBytes)
	if err != nil {
		return EmptyNotarization{}, err
	}

	return EmptyNotarization{
		Vote: emptyVote,
		QC:   qc,
	}, nil
}

// NewNotarization builds a Notarization for a block described by [blockHeader] from [votesForCurrentRound].
func NewNotarization(logger Logger, signatureAggregator SignatureAggregator, votesForCurrentRound map[string]*Vote, blockHeader BlockHeader) (Notarization, error) {
	voteCount := len(votesForCurrentRound)
	signatures := make([]Signature, 0, voteCount)
	logger.Info("Collected Quorum of votes", zap.Uint64("round", blockHeader.Round), zap.Int("votes", voteCount))

	var toBeSignedVote *ToBeSignedVote
	for _, vote := range votesForCurrentRound {
		if !vote.Vote.Equals(&blockHeader) {
			logger.Debug("Skipping vote for block header different than the proposal block header",
				zap.Stringer("voteDigest", &vote.Vote.BlockHeader), zap.Stringer("NodeID", vote.Signature.Signer))
			continue
		}
		logger.Debug("Collected vote from node", zap.Stringer("NodeID", vote.Signature.Signer),
			zap.String("Digest", vote.Vote.Digest.String()), zap.Uint64("round", vote.Vote.Round),
			zap.Uint64("seq", vote.Vote.Seq))
		signatures = append(signatures, vote.Signature)
		if toBeSignedVote == nil {
			toBeSignedVote = &vote.Vote
		}
	}

	if toBeSignedVote == nil {
		return Notarization{}, ErrorNoVotes
	}
	// sort the signatures by Signer to ensure consistent ordering
	slices.SortFunc(signatures, compareSignatures)

	var notarization Notarization
	var err error
	notarization.Vote = *toBeSignedVote
	notarization.QC, err = signatureAggregator.Aggregate(signatures)
	if err != nil {
		return Notarization{}, fmt.Errorf("could not aggregate signatures for notarization: %w", err)
	}

	return notarization, nil
}

// NewFinalization builds a Finalization from [finalizeVotes].
func NewFinalization(signatureAggregator SignatureAggregator, finalizeVotes []*FinalizeVote) (Finalization, error) {
	voteCount := len(finalizeVotes)
	if voteCount == 0 {
		return Finalization{}, ErrorNoVotes
	}

	signatures := make([]Signature, 0, voteCount)
	expectedDigest := finalizeVotes[0].Finalization.Digest
	for _, vote := range finalizeVotes {
		if vote.Finalization.Digest != expectedDigest {
			return Finalization{}, ErrorInvalidFinalizationDigest
		}
		signatures = append(signatures, vote.Signature)
	}

	// sort the signatures, as they are not guaranteed to be in the same order
	slices.SortFunc(signatures, compareSignatures)

	var finalization Finalization
	var err error
	finalization.Finalization = finalizeVotes[0].Finalization
	finalization.QC, err = signatureAggregator.Aggregate(signatures)
	if err != nil {
		return Finalization{}, fmt.Errorf("could not aggregate signatures for finalization: %w", err)
	}

	return finalization, nil
}

// compareSignatures compares two signatures by their Signer field returning -1, 0, 1 if i is less than, equal to, or greater than j.
func compareSignatures(i, j Signature) int {
	return bytes.Compare(i.Signer, j.Signer)
}
