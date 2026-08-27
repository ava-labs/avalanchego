// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"io"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/hook/acp176"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// GetHeaderExtra returns the [HeaderExtra] from the given [Header].
func GetHeaderExtra(h *ethtypes.Header) *HeaderExtra {
	return extras.Header.Get(h)
}

// SetHeaderExtra sets the given [HeaderExtra] on the [Header].
func SetHeaderExtra(h *ethtypes.Header, extra *HeaderExtra) {
	extras.Header.Set(h, extra)
}

// WithHeaderExtra sets the given [HeaderExtra] on the [Header]
// and returns the [Header] for chaining.
func WithHeaderExtra(h *ethtypes.Header, extra *HeaderExtra) *ethtypes.Header {
	SetHeaderExtra(h, extra)
	return h
}

// HeaderExtra is a struct that contains extra fields used by Subnet-EVM
// in the block header.
// This type uses [HeaderSerializable] to encode and decode the extra fields
// along with the upstream type for compatibility with existing network blocks.
type HeaderExtra struct {
	BlockGasCost     *big.Int
	TimeMilliseconds *uint64
	MinDelayExcess   *acp226.DelayExcess
	// TargetExcess and the Settled* fields are populated by the SAE block
	// builder for subnet-evm chains running on top of avalanchego's SAE
	// engine. They are ignored on legacy (non-SAE) chains.
	//
	// The Settled* quartet encodes the SAE settlement marker (see
	// hook.Settled): the height of the block settled by this header and the
	// gas-time coordinates of that block's post-execution state. All four
	// MUST be set together; a header with any of them missing is treated as
	// synchronously executed (pre-SAE).
	TargetExcess        *acp176.TargetExcess
	SettledHeight       *uint64
	SettledGasUnix      *uint64
	SettledGasNumerator *uint64
	SettledExcess       *uint64

	// The GasConfig* quintet encodes the effective ACP-224 gas configuration
	// derived from gaspricemanager precompile storage in the settled state,
	// stamped by the SAE block builder's FinalizeHeader when the precompile
	// is enabled at the settled timestamp. All five MUST be set together;
	// their absence means ACP-176 defaults apply. Boolean values are encoded
	// as 0/1.
	GasConfigValidatorTargetGas    *uint64
	GasConfigTargetGas             *uint64
	GasConfigTargetToExcessScaling *uint64
	GasConfigMinGasPrice           *uint64
	GasConfigStaticPricing         *uint64
}

// HeaderTimeMilliseconds returns the header timestamp in milliseconds.
// If the header has the Granite field TimeMilliseconds set in extras, it is used.
// Otherwise, it falls back to seconds-based Time multiplied by 1000.
func HeaderTimeMilliseconds(h *ethtypes.Header) uint64 {
	extra := GetHeaderExtra(h)
	if extra != nil && extra.TimeMilliseconds != nil {
		return *extra.TimeMilliseconds
	}
	return h.Time * 1000
}

// EncodeRLP RLP encodes the given [ethtypes.Header] and [HeaderExtra] together
// to the `writer`. It does merge both structs into a single [HeaderSerializable].
func (h *HeaderExtra) EncodeRLP(eth *ethtypes.Header, writer io.Writer) error {
	temp := new(HeaderSerializable)

	temp.updateFromEth(eth)
	temp.updateFromExtras(h)

	return rlp.Encode(writer, temp)
}

// DecodeRLP RLP decodes from the [*rlp.Stream] and writes the output to both the
// [ethtypes.Header] passed as argument and to the receiver [HeaderExtra].
func (h *HeaderExtra) DecodeRLP(eth *ethtypes.Header, stream *rlp.Stream) error {
	temp := new(HeaderSerializable)
	if err := stream.Decode(temp); err != nil {
		return err
	}

	temp.updateToEth(eth)
	temp.updateToExtras(h)

	return nil
}

// EncodeJSON JSON encodes the given [ethtypes.Header] and [HeaderExtra] together
// to the `writer`. It does merge both structs into a single [HeaderSerializable].
func (h *HeaderExtra) EncodeJSON(eth *ethtypes.Header) ([]byte, error) {
	temp := new(HeaderSerializable)

	temp.updateFromEth(eth)
	temp.updateFromExtras(h)

	return temp.MarshalJSON()
}

// DecodeJSON JSON decodes from the `input` bytes and writes the output to both the
// [ethtypes.Header] passed as argument and to the receiver [HeaderExtra].
func (h *HeaderExtra) DecodeJSON(eth *ethtypes.Header, input []byte) error {
	temp := new(HeaderSerializable)
	if err := temp.UnmarshalJSON(input); err != nil {
		return err
	}

	temp.updateToEth(eth)
	temp.updateToExtras(h)

	return nil
}

func (h *HeaderExtra) PostCopy(dst *ethtypes.Header) {
	cp := &HeaderExtra{}
	if h.BlockGasCost != nil {
		cp.BlockGasCost = new(big.Int).Set(h.BlockGasCost)
	}
	if h.TimeMilliseconds != nil {
		m := *h.TimeMilliseconds
		cp.TimeMilliseconds = &m
	}
	if h.MinDelayExcess != nil {
		e := *h.MinDelayExcess
		cp.MinDelayExcess = &e
	}
	if h.TargetExcess != nil {
		e := *h.TargetExcess
		cp.TargetExcess = &e
	}
	copyUint64Ptr := func(v *uint64) *uint64 {
		if v == nil {
			return nil
		}
		c := *v
		return &c
	}
	cp.SettledHeight = copyUint64Ptr(h.SettledHeight)
	cp.SettledGasUnix = copyUint64Ptr(h.SettledGasUnix)
	cp.SettledGasNumerator = copyUint64Ptr(h.SettledGasNumerator)
	cp.SettledExcess = copyUint64Ptr(h.SettledExcess)
	cp.GasConfigValidatorTargetGas = copyUint64Ptr(h.GasConfigValidatorTargetGas)
	cp.GasConfigTargetGas = copyUint64Ptr(h.GasConfigTargetGas)
	cp.GasConfigTargetToExcessScaling = copyUint64Ptr(h.GasConfigTargetToExcessScaling)
	cp.GasConfigMinGasPrice = copyUint64Ptr(h.GasConfigMinGasPrice)
	cp.GasConfigStaticPricing = copyUint64Ptr(h.GasConfigStaticPricing)
	SetHeaderExtra(dst, cp)
}

func (h *HeaderExtra) PostRPCMarshal(_ *ethtypes.Header, m map[string]any) {
	if h.BlockGasCost != nil {
		m["blockGasCost"] = (*hexutil.Big)(h.BlockGasCost)
	}
	if h.TimeMilliseconds != nil {
		m["timestampMilliseconds"] = hexutil.Uint64(*h.TimeMilliseconds)
	}
	if h.MinDelayExcess != nil {
		m["minDelayExcess"] = hexutil.Uint64(*h.MinDelayExcess)
	}
}

func (h *HeaderSerializable) updateFromEth(eth *ethtypes.Header) {
	h.ParentHash = eth.ParentHash
	h.UncleHash = eth.UncleHash
	h.Coinbase = eth.Coinbase
	h.Root = eth.Root
	h.TxHash = eth.TxHash
	h.ReceiptHash = eth.ReceiptHash
	h.Bloom = eth.Bloom
	h.Difficulty = eth.Difficulty
	h.Number = eth.Number
	h.GasLimit = eth.GasLimit
	h.GasUsed = eth.GasUsed
	h.Time = eth.Time
	h.Extra = eth.Extra
	h.MixDigest = eth.MixDigest
	h.Nonce = eth.Nonce
	h.BaseFee = eth.BaseFee
	h.BlobGasUsed = eth.BlobGasUsed
	h.ExcessBlobGas = eth.ExcessBlobGas
	h.ParentBeaconRoot = eth.ParentBeaconRoot
}

func (h *HeaderSerializable) updateToEth(eth *ethtypes.Header) {
	eth.ParentHash = h.ParentHash
	eth.UncleHash = h.UncleHash
	eth.Coinbase = h.Coinbase
	eth.Root = h.Root
	eth.TxHash = h.TxHash
	eth.ReceiptHash = h.ReceiptHash
	eth.Bloom = h.Bloom
	eth.Difficulty = h.Difficulty
	eth.Number = h.Number
	eth.GasLimit = h.GasLimit
	eth.GasUsed = h.GasUsed
	eth.Time = h.Time
	eth.Extra = h.Extra
	eth.MixDigest = h.MixDigest
	eth.Nonce = h.Nonce
	eth.BaseFee = h.BaseFee
	eth.BlobGasUsed = h.BlobGasUsed
	eth.ExcessBlobGas = h.ExcessBlobGas
	eth.ParentBeaconRoot = h.ParentBeaconRoot
}

func (h *HeaderSerializable) updateFromExtras(extras *HeaderExtra) {
	h.BlockGasCost = extras.BlockGasCost
	h.TimeMilliseconds = extras.TimeMilliseconds
	h.MinDelayExcess = (*uint64)(extras.MinDelayExcess)
	h.TargetExcess = (*uint64)(extras.TargetExcess)
	h.SettledHeight = extras.SettledHeight
	h.SettledGasUnix = extras.SettledGasUnix
	h.SettledGasNumerator = extras.SettledGasNumerator
	h.SettledExcess = extras.SettledExcess
	h.GasConfigValidatorTargetGas = extras.GasConfigValidatorTargetGas
	h.GasConfigTargetGas = extras.GasConfigTargetGas
	h.GasConfigTargetToExcessScaling = extras.GasConfigTargetToExcessScaling
	h.GasConfigMinGasPrice = extras.GasConfigMinGasPrice
	h.GasConfigStaticPricing = extras.GasConfigStaticPricing
}

func (h *HeaderSerializable) updateToExtras(extras *HeaderExtra) {
	extras.BlockGasCost = h.BlockGasCost
	extras.TimeMilliseconds = h.TimeMilliseconds
	extras.MinDelayExcess = (*acp226.DelayExcess)(h.MinDelayExcess)
	extras.TargetExcess = (*acp176.TargetExcess)(h.TargetExcess)
	extras.SettledHeight = h.SettledHeight
	extras.SettledGasUnix = h.SettledGasUnix
	extras.SettledGasNumerator = h.SettledGasNumerator
	extras.SettledExcess = h.SettledExcess
	extras.GasConfigValidatorTargetGas = h.GasConfigValidatorTargetGas
	extras.GasConfigTargetGas = h.GasConfigTargetGas
	extras.GasConfigTargetToExcessScaling = h.GasConfigTargetToExcessScaling
	extras.GasConfigMinGasPrice = h.GasConfigMinGasPrice
	extras.GasConfigStaticPricing = h.GasConfigStaticPricing
}

// NOTE: both generators currently do not support type aliases.
// We are using custom versions of these programs for now to support type aliases,
// see https://github.com/ava-labs/coreth/pull/746#discussion_r1969673252
//go:generate go tool gencodec -type HeaderSerializable -field-override headerMarshaling -out gen_header_serializable_json.go
//go:generate go tool rlpgen -type HeaderSerializable -out gen_header_serializable_rlp.go

// HeaderSerializable defines the header of a block in the Ethereum blockchain,
// as it is to be serialized into RLP and JSON. Note it must be exported so that
// rlpgen can generate the serialization code from it.
//
//nolint:tagalign
type HeaderSerializable struct {
	ParentHash  common.Hash         `json:"parentHash"       gencodec:"required"`
	UncleHash   common.Hash         `json:"sha3Uncles"       gencodec:"required"`
	Coinbase    common.Address      `json:"miner"            gencodec:"required"`
	Root        common.Hash         `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash         `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash         `json:"receiptsRoot"     gencodec:"required"`
	Bloom       ethtypes.Bloom      `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int            `json:"difficulty"       gencodec:"required"`
	Number      *big.Int            `json:"number"           gencodec:"required"`
	GasLimit    uint64              `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64              `json:"gasUsed"          gencodec:"required"`
	Time        uint64              `json:"timestamp"        gencodec:"required"`
	Extra       []byte              `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash         `json:"mixHash"`
	Nonce       ethtypes.BlockNonce `json:"nonce"`

	// BaseFee was added by EIP-1559 and is ignored in legacy headers.
	BaseFee *big.Int `json:"baseFeePerGas" rlp:"optional"`

	// BlockGasCost was added by SubnetEVM and is ignored in legacy
	// headers.
	BlockGasCost *big.Int `json:"blockGasCost" rlp:"optional"`

	// BlobGasUsed was added by EIP-4844 and is ignored in legacy headers.
	BlobGasUsed *uint64 `json:"blobGasUsed" rlp:"optional"`

	// ExcessBlobGas was added by EIP-4844 and is ignored in legacy headers.
	ExcessBlobGas *uint64 `json:"excessBlobGas" rlp:"optional"`

	// ParentBeaconRoot was added by EIP-4788 and is ignored in legacy headers.
	ParentBeaconRoot *common.Hash `json:"parentBeaconBlockRoot" rlp:"optional"`

	// TimeMilliseconds was added by Granite and is ignored in legacy headers.
	TimeMilliseconds *uint64 `json:"timestampMilliseconds" rlp:"optional"`

	// MinDelayExcess was added by Granite and is ignored in legacy headers.
	// We use *uint64 type here to avoid rlpgen generating incorrect code
	MinDelayExcess *uint64 `json:"minDelayExcess" rlp:"optional"`

	// TargetExcess was added by Helicon (SAE) and is ignored in legacy headers.
	TargetExcess *uint64 `json:"targetExcess" rlp:"optional"`

	// SettledHeight was added by Helicon (SAE) and is ignored in legacy headers.
	SettledHeight *uint64 `json:"settledHeight" rlp:"optional"`

	// SettledGasUnix, SettledGasNumerator and SettledExcess complete the SAE
	// settlement marker (see [HeaderExtra]) and are ignored in legacy headers.
	SettledGasUnix      *uint64 `json:"settledGasUnix" rlp:"optional"`
	SettledGasNumerator *uint64 `json:"settledGasNumerator" rlp:"optional"`
	SettledExcess       *uint64 `json:"settledExcess" rlp:"optional"`

	// The GasConfig* quintet carries the effective ACP-224 gas configuration
	// (see [HeaderExtra]) and is ignored in legacy headers.
	GasConfigValidatorTargetGas    *uint64 `json:"gasConfigValidatorTargetGas" rlp:"optional"`
	GasConfigTargetGas             *uint64 `json:"gasConfigTargetGas" rlp:"optional"`
	GasConfigTargetToExcessScaling *uint64 `json:"gasConfigTargetToExcessScaling" rlp:"optional"`
	GasConfigMinGasPrice           *uint64 `json:"gasConfigMinGasPrice" rlp:"optional"`
	GasConfigStaticPricing         *uint64 `json:"gasConfigStaticPricing" rlp:"optional"`
}

// field type overrides for gencodec
type headerMarshaling struct {
	Difficulty                     *hexutil.Big
	Number                         *hexutil.Big
	GasLimit                       hexutil.Uint64
	GasUsed                        hexutil.Uint64
	Time                           hexutil.Uint64
	Extra                          hexutil.Bytes
	BaseFee                        *hexutil.Big
	BlockGasCost                   *hexutil.Big
	Hash                           common.Hash `json:"hash"` // adds call to Hash() in MarshalJSON
	BlobGasUsed                    *hexutil.Uint64
	ExcessBlobGas                  *hexutil.Uint64
	TimeMilliseconds               *hexutil.Uint64
	MinDelayExcess                 *hexutil.Uint64
	TargetExcess                   *hexutil.Uint64
	SettledHeight                  *hexutil.Uint64
	SettledGasUnix                 *hexutil.Uint64
	SettledGasNumerator            *hexutil.Uint64
	SettledExcess                  *hexutil.Uint64
	GasConfigValidatorTargetGas    *hexutil.Uint64
	GasConfigTargetGas             *hexutil.Uint64
	GasConfigTargetToExcessScaling *hexutil.Uint64
	GasConfigMinGasPrice           *hexutil.Uint64
	GasConfigStaticPricing         *hexutil.Uint64
}

// Hash returns the block hash of the header, which is simply the keccak256 hash of its
// RLP encoding.
// This function MUST be exported and is used in [HeaderSerializable.EncodeJSON] which is
// generated to the file gen_header_json.go.
func (h *HeaderSerializable) Hash() common.Hash {
	return ethtypes.RLPHash(h)
}
