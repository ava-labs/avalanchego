// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warptest

import (
	"encoding/base64"
	"fmt"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// AggregateSignatures signs msg with every validator in vdrs.
func AggregateSignatures(
	network *tmpnet.Network,
	vdrs validators.WarpSet,
	msg *warp.UnsignedMessage,
) (*warp.Message, error) {
	msgBytes := msg.Bytes()
	signers := set.NewBits()
	sigs := make([]*bls.Signature, 0, len(vdrs.Validators))
	for i, vdr := range vdrs.Validators {
		nodeID := vdr.NodeIDs[0]
		node, err := network.GetNode(nodeID)
		if err != nil {
			return nil, fmt.Errorf("getting validator %s: %w", nodeID, err)
		}

		// ponytail: tmpnet owns these keys, so signing locally avoids a
		// test-only P2P client.
		keyBytes, err := base64.StdEncoding.DecodeString(node.Flags[config.StakingSignerKeyContentKey])
		if err != nil {
			return nil, fmt.Errorf("decoding signing key for %s: %w", nodeID, err)
		}
		signer, err := localsigner.FromBytes(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parsing signing key for %s: %w", nodeID, err)
		}
		sig, err := signer.Sign(msgBytes)
		if err != nil {
			return nil, fmt.Errorf("signing message with %s: %w", nodeID, err)
		}
		if !bls.Verify(vdr.PublicKey, sig, msgBytes) {
			return nil, fmt.Errorf("validator %s signing key does not match its public key", nodeID)
		}
		signers.Add(i)
		sigs = append(sigs, sig)
	}

	aggSig, err := bls.AggregateSignatures(sigs)
	if err != nil {
		return nil, fmt.Errorf("aggregating signatures: %w", err)
	}
	return warp.NewMessage(msg, &warp.BitSetSignature{
		Signers:   signers.Bytes(),
		Signature: [bls.SignatureLen]byte(bls.SignatureToBytes(aggSig)),
	})
}
