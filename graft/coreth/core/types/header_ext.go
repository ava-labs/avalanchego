// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"io"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// GetHeaderExtra returns the [HeaderExtra] from the given [Header].
func GetHeaderExtra(h *Header) *HeaderExtra {
	return extras.Header.Get(h)
}

// SetHeaderExtra sets the given [HeaderExtra] on the [Header].
func SetHeaderExtra(h *Header, extra *HeaderExtra) {
	extras.Header.Set(h, extra)
}

// HeaderExtra is a struct that contains extra fields used by Avalanche
// in the block header.
// This type uses [HeaderSerializable] to encode and decode the extra fields
// along with the upstream type for compatibility with existing network blocks.
type HeaderExtra struct {
	ExtDataHash    common.Hash
	ExtDataGasUsed *big.Int
	BlockGasCost   *big.Int
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
	panic("not implemented")
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
	h.ExtDataHash = extras.ExtDataHash
	h.ExtDataGasUsed = extras.ExtDataGasUsed
	h.BlockGasCost = extras.BlockGasCost
}

func (h *HeaderSerializable) updateToExtras(extras *HeaderExtra) {
	extras.ExtDataHash = h.ExtDataHash
	extras.ExtDataGasUsed = h.ExtDataGasUsed
	extras.BlockGasCost = h.BlockGasCost
}

// NOTE: both generators currently do not support type aliases.
// We are using custom versions of these programs for now to support type aliases,
// see https://github.com/ava-labs/coreth/pull/746#discussion_r1969673252
//go:generate go run github.com/fjl/gencodec -type HeaderSerializable -field-override headerMarshaling -out gen_header_serializable_json.go
//go:generate go run github.com/ava-labs/libevm/rlp/rlpgen -type HeaderSerializable -out gen_header_serializable_rlp.go

// HeaderSerializable defines the header of a block in the Ethereum blockchain,
// as it is to be serialized into RLP and JSON. Note it must be exported so that
// rlpgen can generate the serialization code from it.
type HeaderSerializable struct {
	ParentHash  common.Hash    `json:"parentHash"       gencodec:"required"`
	UncleHash   common.Hash    `json:"sha3Uncles"       gencodec:"required"`
	Coinbase    common.Address `json:"miner"            gencodec:"required"`
	Root        common.Hash    `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash    `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash    `json:"receiptsRoot"     gencodec:"required"`
	Bloom       Bloom          `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int       `json:"difficulty"       gencodec:"required"`
	Number      *big.Int       `json:"number"           gencodec:"required"`
	GasLimit    uint64         `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64         `json:"gasUsed"          gencodec:"required"`
	Time        uint64         `json:"timestamp"        gencodec:"required"`
	Extra       []byte         `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash    `json:"mixHash"`
	Nonce       BlockNonce     `json:"nonce"`
	ExtDataHash common.Hash    `json:"extDataHash"      gencodec:"required"`

	// BaseFee was added by EIP-1559 and is ignored in legacy headers.
	BaseFee *big.Int `json:"baseFeePerGas" rlp:"optional"`

	// ExtDataGasUsed was added by Apricot Phase 4 and is ignored in legacy
	// headers.
	//
	// It is not a uint64 like GasLimit or GasUsed because it is not possible to
	// correctly encode this field optionally with uint64.
	ExtDataGasUsed *big.Int `json:"extDataGasUsed" rlp:"optional"`

	// BlockGasCost was added by Apricot Phase 4 and is ignored in legacy
	// headers.
	BlockGasCost *big.Int `json:"blockGasCost" rlp:"optional"`

	// BlobGasUsed was added by EIP-4844 and is ignored in legacy headers.
	BlobGasUsed *uint64 `json:"blobGasUsed" rlp:"optional"`

	// ExcessBlobGas was added by EIP-4844 and is ignored in legacy headers.
	ExcessBlobGas *uint64 `json:"excessBlobGas" rlp:"optional"`

	// ParentBeaconRoot was added by EIP-4788 and is ignored in legacy headers.
	ParentBeaconRoot *common.Hash `json:"parentBeaconBlockRoot" rlp:"optional"`
}

// field type overrides for gencodec
type headerMarshaling struct {
	Difficulty     *hexutil.Big
	Number         *hexutil.Big
	GasLimit       hexutil.Uint64
	GasUsed        hexutil.Uint64
	Time           hexutil.Uint64
	Extra          hexutil.Bytes
	BaseFee        *hexutil.Big
	ExtDataGasUsed *hexutil.Big
	BlockGasCost   *hexutil.Big
	Hash           common.Hash `json:"hash"` // adds call to Hash() in MarshalJSON
	BlobGasUsed    *hexutil.Uint64
	ExcessBlobGas  *hexutil.Uint64
}

// Hash returns the block hash of the header, which is simply the keccak256 hash of its
// RLP encoding.
// This function MUST be exported and is used in [HeaderSerializable.EncodeJSON] which is
// generated to the file gen_header_json.go.
func (h *HeaderSerializable) Hash() common.Hash {
	return rlpHash(h)
}
