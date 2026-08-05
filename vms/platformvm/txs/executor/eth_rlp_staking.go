// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
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

// EthSpend is the outcome of input selection: which UTXOs the tx consumes, how
// much they hold, and the gas and fee that consuming exactly that many implies.
type EthSpend struct {
	Consumed []*avax.UTXO
	Total    uint64
	Gas      gas.Gas
	Fee      uint64
}

// EthSpenderState is the chain view input selection reads.
type EthSpenderState interface {
	avax.UTXOReader

	GetTimestamp() time.Time
}

// EthSpender resolves eth input selection. The executor and eth_estimateGas
// share it, so an estimate is the number execution will charge given the same
// state.
type EthSpender struct {
	state         EthSpenderState
	weights       gas.Dimensions
	avaxAssetID   ids.ID
	feeCalculator fee.Calculator
}

func NewEthSpender(
	state EthSpenderState,
	weights gas.Dimensions,
	avaxAssetID ids.ID,
	feeCalculator fee.Calculator,
) *EthSpender {
	return &EthSpender{
		state:         state,
		weights:       weights,
		avaxAssetID:   avaxAssetID,
		feeCalculator: feeCalculator,
	}
}

// selectEthInputs decides how an eth tx pays for [need] plus its own fee.
//
// Gas depends on the number of inputs consumed and the fee depends on gas, so
// the requirement moves as inputs are added. The walk resolves that directly:
// candidates are ordered by amount descending (ties by UTXO ID ascending), and
// for each prefix of length n it tests the requirement computed for exactly n
// inputs. It stops at the first n that holds, and the fee charged is the fee
// that n implies, so the stopping condition and the charge can never disagree.
//
// It terminates and finds the minimum n:
//   - each added input raises the running total by its amount and raises the
//     requirement by a constant (one read plus one write, priced), so the test
//     is not circular, only monotone;
//   - taking the largest amounts first maximizes the total for every n, so if
//     any n-subset can cover the requirement for n inputs, the prefix does.
//     Scanning n upward therefore finds the smallest workable input count.
//
// Ordering by amount is also what makes accounts unbrickable: UTXO IDs are
// grindable offline, so any ID-first order lets an attacker plant ground low-ID
// dust that displaces real funds. Amount-first means dust never displaces
// value.
//
// ponytail: the candidate scan is O(the sender's UTXO count) while gas prices
// only the inputs consumed, because ordering by amount requires reading every
// candidate. Bounding the scan without reintroducing grindability needs an
// amount-ordered index, which is an ACP-level open item.
func (e *EthSpender) SelectInputs(sender ids.ShortID, need uint64, rlpLen int, gasLimit uint64) (EthSpend, error) {
	candidates, err := e.candidates(sender)
	if err != nil {
		return EthSpend{}, err
	}

	maxInputs := min(len(candidates), txs.MaxEthRLPTxInputs)
	var total uint64
	for n := 1; n <= maxInputs; n++ {
		total, err = safemath.Add(total, amountOf(candidates[n-1]))
		if err != nil {
			return EthSpend{}, err
		}

		txGas, err := e.gasFor(rlpLen, n)
		if err != nil {
			return EthSpend{}, err
		}
		if uint64(txGas) > gasLimit {
			// Everything from here on costs more gas, so the tx cannot pay for
			// the inputs this account's fragmentation requires.
			return EthSpend{}, fmt.Errorf(
				"%w: %d inputs need %d gas, limit is %d",
				errEthGasLimitTooLowForInputs, n, txGas, gasLimit,
			)
		}
		txFee, err := e.feeCalculator.CalculateFeeForGas(txGas)
		if err != nil {
			return EthSpend{}, err
		}
		required, err := safemath.Add(need, txFee)
		if err != nil {
			return EthSpend{}, err
		}
		if total >= required {
			return EthSpend{
				Consumed: candidates[:n],
				Total:    total,
				Gas:      txGas,
				Fee:      txFee,
			}, nil
		}
	}

	// Nothing coverable. Distinguish "not enough AVAX at all" from "enough AVAX
	// but too fragmented for one tx", because only the second is fixed by
	// consolidating or raising the gas limit.
	if len(candidates) > txs.MaxEthRLPTxInputs {
		fullTotal := total
		for _, utxo := range candidates[maxInputs:] {
			fullTotal, err = safemath.Add(fullTotal, amountOf(utxo))
			if err != nil {
				return EthSpend{}, err
			}
		}
		if fullTotal > total {
			return EthSpend{}, fmt.Errorf(
				"%w: %d UTXOs hold %d nAVAX but one tx may consume only %d of them",
				errEthTooFragmented, len(candidates), fullTotal, txs.MaxEthRLPTxInputs,
			)
		}
	}
	return EthSpend{}, fmt.Errorf("%w: have %d nAVAX, need %d plus fee", errEthInsufficientFunds, total, need)
}

// ethCandidates returns the sender's spendable AVAX UTXOs, deduplicated and
// ordered by amount descending then UTXO ID ascending.
func (e *EthSpender) candidates(sender ids.ShortID) ([]*avax.UTXO, error) {
	utxoIDs, err := e.state.UTXOIDs(sender.Bytes(), ids.Empty, math.MaxInt)
	if err != nil {
		return nil, err
	}

	var (
		chainTime  = uint64(e.state.GetTimestamp().Unix())
		seen       = set.NewSet[ids.ID](len(utxoIDs))
		candidates = make([]*avax.UTXO, 0, len(utxoIDs))
	)
	for _, utxoID := range utxoIDs {
		// UTXOIDs may report the same UTXO twice (the diff merges its own
		// additions into the parent's index), and consuming a duplicate would
		// count its amount twice while deleting it once.
		if seen.Contains(utxoID) {
			continue
		}
		seen.Add(utxoID)

		utxo, err := e.state.GetUTXO(utxoID)
		if err != nil {
			return nil, err
		}
		if utxo.AssetID() != e.avaxAssetID {
			continue
		}
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		if out.Locktime > chainTime ||
			out.Threshold != 1 ||
			len(out.Addrs) != 1 ||
			out.Addrs[0] != sender {
			continue
		}
		candidates = append(candidates, utxo)
	}

	sort.Sort(byAmountThenID(candidates))
	return candidates, nil
}

func (e *EthSpender) gasFor(rlpLen int, numInputs int) (gas.Gas, error) {
	return fee.EthRLPTxComplexity(rlpLen, numInputs).ToGas(e.weights)
}

func amountOf(utxo *avax.UTXO) uint64 {
	return utxo.Out.(*secp256k1fx.TransferOutput).Amt
}

// byAmountThenID sorts UTXOs by amount descending, then by UTXO ID ascending.
type byAmountThenID []*avax.UTXO

func (u byAmountThenID) Len() int      { return len(u) }
func (u byAmountThenID) Swap(i, j int) { u[i], u[j] = u[j], u[i] }

func (u byAmountThenID) Less(i, j int) bool {
	iAmt := u[i].Out.(*secp256k1fx.TransferOutput).Amt
	jAmt := u[j].Out.(*secp256k1fx.TransferOutput).Amt
	if iAmt != jAmt {
		return iAmt > jAmt
	}
	iID, jID := u[i].InputID(), u[j].InputID()
	return iID.Compare(jID) < 0
}

// ethSpender builds the selector for this execution's state and fee schedule.
func (e *standardTxExecutor) ethSpender() *EthSpender {
	return NewEthSpender(
		e.state,
		e.backend.Config.DynamicFeeConfig.Weights,
		e.backend.Ctx.AVAXAssetID,
		e.feeCalculator,
	)
}
