// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"fmt"
	"sync"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

// ApprovalStore holds the latest valid approval received from each validator of the next epoch.
type ApprovalStore struct {
	signatureVerifier common.SignatureVerifier
	validators        NodeBLSMappings
	logger            common.Logger
	nodeIDToPK        map[avalanchego.NodeID][]byte
	// lock guards approvalsByNode: approvals are handled as they arrive while
	// the block builder reads them when building a block.
	lock            sync.RWMutex
	approvalsByNode map[avalanchego.NodeID]common.ValidatorSetApproval
}

func NewApprovalStore(signatureVerifier common.SignatureVerifier, validators NodeBLSMappings, logger common.Logger) *ApprovalStore {
	pkByNodeID := make(map[avalanchego.NodeID][]byte, len(validators))
	for _, vdr := range validators {
		pkByNodeID[vdr.NodeID] = vdr.BLSKey
	}

	return &ApprovalStore{
		signatureVerifier: signatureVerifier,
		validators:        validators,
		logger:            logger,
		nodeIDToPK:        pkByNodeID,
		approvalsByNode:   make(map[avalanchego.NodeID]common.ValidatorSetApproval, len(validators)),
	}
}

func (as *ApprovalStore) Approvals() ValidatorSetApprovals {
	as.lock.RLock()
	defer as.lock.RUnlock()

	approvals := make(ValidatorSetApprovals, 0, len(as.approvalsByNode))
	for _, approval := range as.approvalsByNode {
		approvals = append(approvals, approval)
	}
	return approvals
}

// HandleApproval stores the approval if it is signed by a validator, replacing any earlier approval from that node.
func (as *ApprovalStore) HandleApproval(approval *common.ValidatorSetApproval) {
	pk, exists := as.nodeIDToPK[approval.NodeID]
	if !exists {
		as.logger.Debug("Received an approval from a node that is not a validator", zap.String("nodeID",
			fmt.Sprintf("%x", approval.NodeID)), zap.Uint64("pChainHeight", approval.PChainHeight))
		return
	}

	// The signature must be valid for nodes to be able to aggregate it later on along with other approvals.
	// This is checked before taking the lock, as it only reads immutable state.
	if err := as.checkApprovalSignature(approval, pk); err != nil {
		as.logger.Debug("Received an approval with an invalid signature", zap.String("nodeID",
			fmt.Sprintf("%x", approval.NodeID)), zap.Uint64("pChainHeight", approval.PChainHeight))
		return
	}

	as.lock.Lock()
	defer as.lock.Unlock()

	as.approvalsByNode[approval.NodeID] = *approval
}

func (as *ApprovalStore) checkApprovalSignature(approval *common.ValidatorSetApproval, pk common.PublicKeyBytes) error {
	toBeSigned, err := assembleApprovalToBeSigned(approval.PChainHeight, approval.AuxInfoDigest)
	if err != nil {
		return err
	}
	return as.signatureVerifier.VerifySignature(toBeSigned, approval.Signature, pk)
}
