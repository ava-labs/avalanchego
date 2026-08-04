// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"math/big"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	// EthRLPChainID is the EIP-155 chain ID of the P-chain eth facade.
	// ponytail: fixed constant; production needs per-network IDs.
	EthRLPChainID = 43117

	// MaxEthRLPTxInputs bounds input auto-selection: execution considers only
	// the sender's MaxEthRLPTxInputs lowest-ID UTXOs. Complexity prices reads
	// and deletes at this worst case.
	MaxEthRLPTxInputs = 32
)

// WeiPerNAVAX converts the 18-decimal RLP value field to 9-decimal nAVAX.
var WeiPerNAVAX = big.NewInt(1_000_000_000)

var (
	_ UnsignedTx = (*EthRLPTx)(nil)

	ErrNotDynamicFeeTx    = errors.New("only type-2 (dynamic fee) eth txs are supported")
	ErrNonEmptyAccessList = errors.New("access lists are not supported")
	ErrWrongEthChainID    = errors.New("wrong eth chain ID")
	errNoRecipient        = errors.New("eth tx must have a recipient")
	ErrNonEmptyCalldata   = errors.New("plain transfers must have empty calldata")
	errNonPositiveValue   = errors.New("value must be positive")
	ErrValueDust          = errors.New("value must be a whole number of nAVAX (multiple of 1e9 wei)")
	errValueTooLarge      = errors.New("value overflows uint64 nAVAX")
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

func (tx *EthRLPTx) SyntacticVerify(*snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Parsed != nil: // already passed syntactic verification
		return nil
	}

	eth := &ethtypes.Transaction{}
	if err := eth.UnmarshalBinary(tx.RLP); err != nil {
		return fmt.Errorf("parsing eth tx: %w", err)
	}
	chainID := big.NewInt(EthRLPChainID)
	switch {
	case eth.Type() != ethtypes.DynamicFeeTxType:
		return ErrNotDynamicFeeTx
	case len(eth.AccessList()) != 0:
		// Keeps the envelope size bounded by a constant; see EthRLPTxComplexity.
		return ErrNonEmptyAccessList
	case eth.ChainId().Cmp(chainID) != 0:
		return fmt.Errorf("%w: got %s, want %d", ErrWrongEthChainID, eth.ChainId(), EthRLPChainID)
	case eth.To() == nil:
		return errNoRecipient
	case len(eth.Data()) != 0:
		return ErrNonEmptyCalldata
	case eth.Value().Sign() <= 0:
		return errNonPositiveValue
	}

	amount, rem := new(big.Int).QuoRem(eth.Value(), WeiPerNAVAX, new(big.Int))
	switch {
	case rem.Sign() != 0:
		return ErrValueDust
	case !amount.IsUint64():
		return errValueTooLarge
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
