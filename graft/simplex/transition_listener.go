// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"

	metadata "github.com/ava-labs/simplex/msm"
)

// epochTransitionListener reacts to blocks committed to storage. When a
// transition block is indexed, it performs any tasks required of this node to
// complete the epoch transition, such as sending out approval messages.
// Non-validators should also use this listener, since they may become
// validators after the transition.
type epochTransitionListener struct {
	// sender is used for sending potential approvals and auxiliary information
	// individually to the validators of the next epoch.
	sender Sender

	myNodeID avalanchego.NodeID

	// getValidatorSet returns the validator set at a given P-chain height.
	getValidatorSet metadata.ValidatorSetRetriever
	// getBlock retrieves a previously finalized block, used to traverse the auxiliary info history.
	getBlock metadata.BlockRetriever
	// signer signs epoch transition approvals.
	signer common.Signer
	// auxInfoApp decides whether the auxiliary info history is sufficient and generates new entries.
	auxInfoApp metadata.AuxiliaryInfoGenVerifier
	// handleApproval records our own broadcast approval in the local approval store.
	// It is set for validators (whose MSM builds the next blocks and must include the
	// approval) and nil for non-validators, which have no block builder to feed.
	handleApproval func(approval *common.ValidatorSetApproval)

	logger common.Logger
}

func newEpochTransitionListener(
	logger common.Logger,
	sender Sender,
	myNodeID avalanchego.NodeID,
	getValidatorSet metadata.ValidatorSetRetriever,
	getBlock metadata.BlockRetriever,
	signer common.Signer,
	auxInfoApp metadata.AuxiliaryInfoGenVerifier,
	handleApproval func(approval *common.ValidatorSetApproval),
) *epochTransitionListener {
	return &epochTransitionListener{
		sender:          sender,
		myNodeID:        myNodeID,
		getValidatorSet: getValidatorSet,
		getBlock:        getBlock,
		signer:          signer,
		auxInfoApp:      auxInfoApp,
		handleApproval:  handleApproval,
		logger:          logger,
	}
}

func (a *epochTransitionListener) handleTransitionBlock(block *ParsedBlock) error {
	nextEpochPChainReference := block.Metadata.SimplexEpochInfo.NextPChainReferenceHeight

	// if our node is not in the next validator set, no need to send anything.
	nextEpochValidatorSet, err := a.getValidatorSet(nextEpochPChainReference)
	if err != nil {
		return err
	}

	indexes := nextEpochValidatorSet.IndexByNodeID()
	if _, ok := indexes[a.myNodeID]; !ok {
		return nil // we are not in the next validator set
	}

	auxInfoHistory, err := metadata.GetAuxiliaryHistory(&block.StateMachineBlock, block.BlockHeader().Seq, a.getBlock, a.auxInfoApp.DefaultVersionID())
	if err != nil {
		return err
	}

	isSufficient, err := a.auxInfoApp.IsSufficient(auxInfoHistory.OldestVersionID, nextEpochValidatorSet, auxInfoHistory.Data)
	if err != nil {
		return err
	}

	if isSufficient {
		// no more auxiliary info to send, maybe send our approval
		lastAuxInfoDigest := auxInfoHistory.LastHistoryDigest()
		return a.maybeSendApprovals(block, nextEpochValidatorSet, lastAuxInfoDigest)
	}

	// we need more auxiliary information, attempt to generate
	generatedAuxInfo, err := a.auxInfoApp.Generate(auxInfoHistory.OldestVersionID, nextEpochValidatorSet, auxInfoHistory.Data)
	if err != nil {
		return err
	}

	if generatedAuxInfo == nil {
		return nil
	}

	auxInfoMessage := &common.Message{
		AuxiliaryInfo: &common.AuxiliaryInfo{
			Epoch:   block.BlockHeader().Epoch,
			Version: auxInfoHistory.OldestVersionID,
			Data:    generatedAuxInfo,
		},
	}

	a.sendToValidators(nextEpochValidatorSet, auxInfoMessage)
	return nil
}

// sendToValidators sends the message individually to every validator in the set except ourselves.
func (a *epochTransitionListener) sendToValidators(validators metadata.NodeBLSMappings, msg *common.Message) {
	for _, validator := range validators {
		if validator.NodeID == a.myNodeID {
			continue
		}
		a.sender.Send(msg, common.NodeID(validator.NodeID[:]))
	}
}

func (a *epochTransitionListener) maybeSendApprovals(block *ParsedBlock, nextEpochValidatorSet metadata.NodeBLSMappings, auxInfoDigest common.Digest) error {
	nextEpochPChainReference := block.Metadata.SimplexEpochInfo.NextPChainReferenceHeight

	sig, err := metadata.SignApproval(a.signer, nextEpochPChainReference, auxInfoDigest)
	if err != nil {
		return err
	}

	approval := common.ValidatorSetApproval{
		NodeID:        a.myNodeID,
		PChainHeight:  nextEpochPChainReference,
		AuxInfoDigest: auxInfoDigest,
		Signature:     sig,
	}

	approvalMessage := common.Message{
		EpochTransitionApproval: &approval,
	}

	a.sendToValidators(nextEpochValidatorSet, &approvalMessage)

	// Validators also record their own approval locally so the next block they build
	// includes it. Non-validators have no block builder, so handleApproval is nil.
	if a.handleApproval == nil {
		return nil
	}

	a.handleApproval(&approval)
	return nil
}
