// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errEthStakerAlreadyExists = errors.New("derived eth staker tx already exists")

// The eth facade does not store the eth tx as the staker tx. It derives the
// equivalent native staker tx, registers that under its own ID, and points the
// staker at it. Everything downstream (the staker set, getCurrentValidators,
// the reward path, unstaking) then sees an ordinary
// AddPermissionlessValidatorTx or AddPermissionlessDelegatorTx and needs no
// eth-specific handling.
//
// The derived tx declares the inputs the eth tx consumed, which is what makes
// it unique: the same sender may stake the same amount to the same node twice,
// and only the consumed UTXOs distinguish those two derivations. It declares no
// outputs, because change was produced under the eth tx's own ID, so
// stake-return and reward UTXOs are indexed from zero under the derived ID and
// cannot collide with anything.

// ethStakerTx derives the native staker tx authorized by [tx], spending
// [consumed]. It is a pure function of the eth tx and the selected UTXOs, so
// every node derives identical bytes and therefore an identical txID.
func (e *standardTxExecutor) ethStakerTx(
	tx *txs.EthRLPTx,
	consumed []*avax.UTXO,
) (*txs.Tx, txs.BoundedStaker, error) {
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{tx.Sender},
	}
	stakeOuts := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: e.backend.Ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          tx.AmountNAVAX,
			OutputOwners: *owner,
		},
	}}
	ins := make([]*avax.TransferableInput, len(consumed))
	for i, utxo := range consumed {
		out := utxo.Out.(*secp256k1fx.TransferOutput)
		ins[i] = &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt:   out.Amt,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}
	}
	utils.Sort(ins)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    e.backend.Ctx.NetworkID,
		BlockchainID: e.backend.Ctx.ChainID,
		Ins:          ins,
	}}

	var unsigned txs.UnsignedTx
	switch selector := tx.Selector(); selector {
	case txs.SelectorDelegate:
		args, err := txs.ParseEthDelegate(tx.Parsed.Data())
		if err != nil {
			return nil, nil, err
		}
		unsigned = &txs.AddPermissionlessDelegatorTx{
			BaseTx: baseTx,
			Validator: txs.Validator{
				NodeID: args.NodeID,
				End:    args.EndTime,
				Wght:   tx.AmountNAVAX,
			},
			Subnet:                 constants.PrimaryNetworkID,
			StakeOuts:              stakeOuts,
			DelegationRewardsOwner: owner,
		}
	case txs.SelectorAddValidator:
		args, err := txs.ParseEthAddValidator(tx.Parsed.Data())
		if err != nil {
			return nil, nil, err
		}
		publicKey, err := bls.PublicKeyFromCompressedBytes(args.BLSPublicKey)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing blsPublicKey: %w", err)
		}
		pop, err := bls.SignatureFromBytes(args.BLSPoP)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing blsPoP: %w", err)
		}
		unsigned = &txs.AddPermissionlessValidatorTx{
			BaseTx: baseTx,
			Validator: txs.Validator{
				NodeID: args.NodeID,
				End:    args.EndTime,
				Wght:   tx.AmountNAVAX,
			},
			Subnet: constants.PrimaryNetworkID,
			Signer: &signer.ProofOfPossession{
				PublicKey: [bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(publicKey)),
				ProofOfPossession: [bls.SignatureLen]byte(
					bls.SignatureToBytes(pop),
				),
			},
			StakeOuts:             stakeOuts,
			ValidatorRewardsOwner: owner,
			DelegatorRewardsOwner: owner,
			DelegationShares:      args.DelegationFeeBips,
		}
	default:
		return nil, nil, fmt.Errorf("%w: %x", txs.ErrUnknownSelector, selector)
	}

	derived, err := txs.NewSigned(unsigned, txs.Codec, nil)
	if err != nil {
		return nil, nil, err
	}
	staker, ok := derived.Unsigned.(txs.BoundedStaker)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %T", ErrWrongTxType, derived.Unsigned)
	}
	return derived, staker, nil
}

// verifyEthStake applies to the derived tx exactly what the native path
// applies: its own syntactic verification (which covers the BLS proof of
// possession and the staker fields) and the shared staking rules from
// staker_tx_verification.go. The flow check is not reused because the eth tx
// pays and stakes from auto-selected inputs, verified by the caller.
func (e *standardTxExecutor) verifyEthStake(derived *txs.Tx) error {
	if err := derived.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}
	if !e.backend.Bootstrapped.Get() {
		return nil
	}
	switch staker := derived.Unsigned.(type) {
	case *txs.AddPermissionlessValidatorTx:
		return verifyAddPermissionlessValidatorRules(e.backend, e.state, staker)
	case *txs.AddPermissionlessDelegatorTx:
		return verifyAddPermissionlessDelegatorRules(e.backend, e.state, staker)
	default:
		return fmt.Errorf("%w: %T", ErrWrongTxType, derived.Unsigned)
	}
}

// putEthStaker registers the derived staker tx and adds the staker.
func (e *standardTxExecutor) putEthStaker(derived *txs.Tx, staker txs.BoundedStaker) error {
	txID := derived.ID()
	if _, _, err := e.state.GetTx(txID); err == nil {
		// Two identical eth txs cannot both be accepted (the nonce rule), so
		// this would mean a derivation collision.
		return fmt.Errorf("%w: %s", errEthStakerAlreadyExists, txID)
	}
	e.state.AddTx(derived, status.Committed)
	return e.putStakerWithTxID(txID, staker)
}
