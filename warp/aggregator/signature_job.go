// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var errInvalidSignature = errors.New("invalid signature")

// SignatureBackend defines the minimum network interface to perform signature aggregation
type SignatureBackend interface {
	// FetchWarpSignature attempts to fetch a BLS Signature from [nodeID] for [unsignedWarpMessage]
	FetchWarpSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error)
}

// signatureJob fetches a single signature using the injected dependency SignatureBackend and returns a verified signature of the requested message.
type signatureJob struct {
	backend SignatureBackend
	msg     *avalancheWarp.UnsignedMessage

	nodeID    ids.NodeID
	publicKey *bls.PublicKey
	weight    uint64
}

func (s *signatureJob) String() string {
	return fmt.Sprintf("(NodeID: %s, UnsignedMsgID: %s)", s.nodeID, s.msg.ID())
}

func newSignatureJob(backend SignatureBackend, validator *avalancheWarp.Validator, msg *avalancheWarp.UnsignedMessage) *signatureJob {
	return &signatureJob{
		backend:   backend,
		msg:       msg,
		nodeID:    validator.NodeIDs[0], // TODO: update from a single nodeID to the original slice and use extra nodeIDs as backup.
		publicKey: validator.PublicKey,
		weight:    validator.Weight,
	}
}

// Execute attempts to fetch the signature from the nodeID specified in this job and then verifies and returns the signature
func (s *signatureJob) Execute(ctx context.Context) (*bls.Signature, error) {
	signature, err := s.backend.FetchWarpSignature(ctx, s.nodeID, s.msg)
	if err != nil {
		return nil, err
	}

	if !bls.Verify(s.publicKey, signature, s.msg.Bytes()) {
		return nil, fmt.Errorf("%w: node %s returned invalid signature %s for msg %s", errInvalidSignature, s.nodeID, hexutil.Bytes(bls.SignatureToBytes(signature)), s.msg.ID())
	}
	return signature, nil
}
