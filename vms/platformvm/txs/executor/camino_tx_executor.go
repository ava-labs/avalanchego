// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errNodeSignatureMissing = errors.New("last signature is not nodeID's signature")

type CaminoStandardTxExecutor struct {
	StandardTxExecutor
}

type CaminoProposalTxExecutor struct {
	ProposalTxExecutor
}

/* TLS certificates build by caminogo contain a secp256k1 signature of the
 * x509 public signed with the nodeIDs private key
 * TX which require nodeID verification (tx with nodeID parameter) must contain
 * an additional signature after the signatures used for input verification.
 * This signature must recover to the nodeID itself to verify that the sender
 * has access to this node specific private key.
 */
func (e *CaminoStandardTxExecutor) verifyNodeSignature(nodeID ids.NodeID) error {
	if state, err := e.State.CaminoGenesisState(); err != nil {
		return err
	} else if state.VerifyNodeSignature {
		if err := e.Backend.Fx.VerifyPermission(
			e.Tx.Unsigned,
			&secp256k1fx.Input{SigIndices: []uint32{0}},
			e.Tx.Creds[len(e.Tx.Creds)-1],
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					ids.ShortID(nodeID),
				},
			},
		); err != nil {
			return fmt.Errorf("%w: %s", errNodeSignatureMissing, err)
		}
	}
	return nil
}

func (e *CaminoStandardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if err := e.verifyNodeSignature(tx.NodeID()); err != nil {
		return err
	}
	return e.StandardTxExecutor.AddValidatorTx(tx)
}

func (e *CaminoStandardTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if err := e.verifyNodeSignature(tx.NodeID()); err != nil {
		return err
	}
	return e.StandardTxExecutor.AddSubnetValidatorTx(tx)
}
