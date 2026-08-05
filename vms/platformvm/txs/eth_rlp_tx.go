// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	// ethRLPChainIDBase is offset by the network ID to produce the facade's
	// EIP-155 chain ID. The network ID is not part of an eth tx's signed
	// preimage, so the chain ID is the only thing that separates networks: a
	// tx signed for one network must never be a valid tx on another.
	ethRLPChainIDBase = 43_110_000

	// MaxEthRLPTxInputs bounds how many UTXOs one tx may consume. Complexity
	// prices reads and deletes at this worst case.
	MaxEthRLPTxInputs = 32

	// MaxEthRLPEnvelopeBytes bounds the serialized length of an EthRLPTx
	// excluding its calldata payload. eth_estimateGas prices with it, because
	// the exact length is unknown before signing, and under charge-the-limit
	// the signer pays for what it prices, so the bound is derived rather than
	// guessed. Every field of a type-2 tx has a fixed maximum width and access
	// lists are rejected, so summing those widths is a hard bound:
	//
	//	  1  tx type byte (0x02)
	//	  4  RLP list header (0xf7+n, n up to 3 length bytes)
	//	  9  chainID              (1 prefix + 8, uint64)
	//	  9  nonce                (1 prefix + 8, uint64)
	//	 33  maxPriorityFeePerGas (1 prefix + 32, uint256)
	//	 33  maxFeePerGas         (1 prefix + 32, uint256)
	//	  9  gas                  (1 prefix + 8, uint64)
	//	 21  to                   (1 prefix + 20)
	//	 33  value                (1 prefix + 32, uint256)
	//	  4  calldata length prefix (payload counted separately)
	//	  1  accessList (empty list, 0xc0)
	//	  1  v (0 or 1)
	//	 33  r                    (1 prefix + 32)
	//	 33  s                    (1 prefix + 32)
	//	---
	//	224
	//
	// A tx signed with every field at its maximum measures 222, verified by
	// TestEthRLPEnvelopeBound, so the derivation is tight to within its two
	// bytes of length-prefix headroom.
	MaxEthRLPEnvelopeBytes = 1 + 4 + 9 + 9 + 33 + 33 + 9 + 21 + 33 + 4 + 1 + 1 + 33 + 33
)

// EthRLPChainID returns the facade's EIP-155 chain ID on [networkID].
func EthRLPChainID(networkID uint32) *big.Int {
	return new(big.Int).SetUint64(ethRLPChainIDBase + uint64(networkID))
}

// WeiPerNAVAX converts the 18-decimal RLP value field to 9-decimal nAVAX.
var WeiPerNAVAX = big.NewInt(1_000_000_000)

var (
	_ UnsignedTx = (*EthRLPTx)(nil)

	ErrNotDynamicFeeTx    = errors.New("only type-2 (dynamic fee) eth txs are supported")
	ErrNonEmptyAccessList = errors.New("access lists are not supported")
	ErrTransferToToken    = errors.New("the staked-position token address cannot receive transactions")
	ErrWrongEthChainID    = errors.New("wrong eth chain ID")
	errNoRecipient        = errors.New("eth tx must have a recipient")
	ErrNonEmptyCalldata   = errors.New("plain transfers must have empty calldata")
	errNonPositiveValue   = errors.New("value must be positive")
	ErrValueDust          = errors.New("value must be a whole number of nAVAX (multiple of 1e9 wei)")
	errValueTooLarge      = errors.New("value overflows uint64 nAVAX")
	ErrMissingContext     = errors.New("eth tx verification requires the chain context")
	ErrNonceTooLarge      = errors.New("nonce must be less than MaxUint64")
	ErrStakeValueRequired = errors.New("staking calls must carry a positive value")
)

// EthRLPTx wraps a signed Ethereum transaction so that stock EVM wallets can
// author P-chain transactions. The only operation supported is a plain value
// transfer: to = recipient (as a 20-byte ShortID), value = amount in wei,
// empty calldata. Inputs are auto-selected from the sender's UTXOs at
// execution; replay protection is the per-address nonce, not UTXO consumption.
type EthRLPTx struct {
	RLP []byte `serialize:"true" json:"rlp"`

	// The fields below are populated by SyntacticVerify.
	Parsed      *ethtypes.Transaction `json:"-"`
	Sender      ids.ShortID           `json:"-"` // EthAddress of the recovered signer
	Recipient   ids.ShortID           `json:"-"`
	AmountNAVAX uint64                `json:"-"`

	unsignedBytes []byte
}

func (tx *EthRLPTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

func (tx *EthRLPTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (*EthRLPTx) InitCtx(*snow.Context) {}

// InputIDs is empty: inputs are selected at execution, so mempool UTXO
// conflict detection does not apply. Conflicts resolve via the nonce rule.
func (*EthRLPTx) InputIDs() set.Set[ids.ID] {
	return nil
}

func (*EthRLPTx) Outputs() []*avax.TransferableOutput {
	return nil
}

// SyntacticVerify verifies everything about the tx that is a function of its
// own bytes plus [ctx.NetworkID]. A verified result is cached, which is safe
// because a node serves exactly one network.
func (tx *EthRLPTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Parsed != nil: // already passed syntactic verification
		return nil
	case ctx == nil:
		return ErrMissingContext
	}

	eth := &ethtypes.Transaction{}
	if err := eth.UnmarshalBinary(tx.RLP); err != nil {
		return fmt.Errorf("parsing eth tx: %w", err)
	}
	chainID := EthRLPChainID(ctx.NetworkID)
	switch {
	case eth.Type() != ethtypes.DynamicFeeTxType:
		return ErrNotDynamicFeeTx
	case len(eth.AccessList()) != 0:
		// Keeps MaxEthRLPEnvelopeBytes a hard bound.
		return ErrNonEmptyAccessList
	case eth.ChainId().Cmp(chainID) != 0:
		return fmt.Errorf("%w: got %s, want %s", ErrWrongEthChainID, eth.ChainId(), chainID)
	case eth.To() == nil:
		return errNoRecipient
	case eth.Nonce() == math.MaxUint64:
		// The accepted nonce is stored as nonce+1, so MaxUint64 would wrap to
		// zero and reset replay protection.
		return ErrNonceTooLarge
	}

	// The staked-position token is read-only: any tx to it would orphan funds.
	if *eth.To() == EthStakedAVAXAddress {
		return ErrTransferToToken
	}

	// Calldata is legal only when targeting the staking system address; every
	// other recipient is a plain transfer.
	isStakingCall := *eth.To() == EthStakingAddress
	switch {
	case !isStakingCall && len(eth.Data()) != 0:
		return ErrNonEmptyCalldata
	case isStakingCall && len(eth.Data()) < 4:
		return ErrShortCalldata
	case eth.Value().Sign() < 0:
		return errNonPositiveValue
	}

	amount, rem := new(big.Int).QuoRem(eth.Value(), WeiPerNAVAX, new(big.Int))
	switch {
	case rem.Sign() != 0:
		return ErrValueDust
	case !amount.IsUint64():
		return errValueTooLarge
	}

	if isStakingCall {
		var selector [4]byte
		copy(selector[:], eth.Data())
		switch selector {
		case SelectorDelegate:
			if _, err := ParseEthDelegate(eth.Data()); err != nil {
				return err
			}
		case SelectorAddValidator:
			if _, err := ParseEthAddValidator(eth.Data()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: %x", ErrUnknownSelector, selector)
		}
		// Both current selectors stake the value they carry. Value positivity
		// is a per-selector rule, not a tx-wide one: a zero-value tx is how a
		// wallet cancels a pending tx, and future selectors need not be paid.
		if amount.Sign() == 0 {
			return ErrStakeValueRequired
		}
	}

	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(chainID), eth)
	if err != nil {
		return fmt.Errorf("recovering eth tx sender: %w", err)
	}

	tx.Sender = ids.ShortID(sender)
	tx.Recipient = ids.ShortID(*eth.To())
	tx.AmountNAVAX = amount.Uint64()
	tx.Parsed = eth
	return nil
}

func (tx *EthRLPTx) Visit(visitor Visitor) error {
	return visitor.EthRLPTx(tx)
}

// GasLimit is the tx's gas: the limit it signed. It is readable from the tx
// bytes with no chain context and no execution, which is what lets block
// accounting price an eth tx exactly like any other tx type.
func (tx *EthRLPTx) GasLimit() (uint64, error) {
	if tx.Parsed != nil {
		return tx.Parsed.Gas(), nil
	}
	eth := new(ethtypes.Transaction)
	if err := eth.UnmarshalBinary(tx.RLP); err != nil {
		return 0, fmt.Errorf("parsing eth tx: %w", err)
	}
	return eth.Gas(), nil
}

// IsStakingCall reports whether this tx targets the staking system address.
// Only meaningful after SyntacticVerify.
func (tx *EthRLPTx) IsStakingCall() bool {
	return tx.Recipient == ids.ShortID(EthStakingAddress)
}

// Selector returns the 4-byte calldata selector of a staking call.
func (tx *EthRLPTx) Selector() [4]byte {
	var selector [4]byte
	copy(selector[:], tx.Parsed.Data())
	return selector
}
