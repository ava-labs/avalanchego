// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/json"
	"errors"
	"io"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// GetHeaderExtra returns the [HeaderExtra] carried by the header.
func GetHeaderExtra(h *ethtypes.Header) *HeaderExtra {
	return extras.Header.Get(h)
}

// WithHeaderExtra sets the [HeaderExtra] on the header and returns it.
func WithHeaderExtra(h *ethtypes.Header, extra *HeaderExtra) *ethtypes.Header {
	extras.Header.Set(h, extra)
	return h
}

// HeaderExtra holds the Avalanche-specific header fields. Its RLP and JSON
// encodings are merged into those of the upstream [ethtypes.Header] so that
// every historical C-Chain block hash still verifies.
type HeaderExtra struct {
	ExtDataHash      common.Hash
	ExtDataGasUsed   *big.Int
	BlockGasCost     *big.Int
	TimeMilliseconds *uint64
	MinDelayExcess   *acp226.DelayExcess
	TargetExponent   *dynamic.TargetExponent
	MinPriceExponent *dynamic.PriceExponent

	SettledHeight       *uint64
	SettledGasUnix      *uint64
	SettledGasNumerator *uint64
	SettledExcess       *uint64
}

// HeaderTimeMilliseconds returns the header timestamp in milliseconds: the
// Granite TimeMilliseconds field when set, otherwise the seconds-based Time.
func HeaderTimeMilliseconds(h *ethtypes.Header) uint64 {
	if t := GetHeaderExtra(h).TimeMilliseconds; t != nil {
		return *t
	}
	return h.Time * 1000
}

// rlpFields is the C-Chain header wire format: the upstream fields in their
// go-ethereum order with ExtDataHash appended, then the fields added by each
// upgrade as optional trailing elements. A nil optional field is omitted when
// every later one is nil too, and encoded as an empty string otherwise.
//
// `ptr` selects pointers to the fields (for decoding) over their values.
func (h *HeaderExtra) rlpFields(eth *ethtypes.Header, ptr bool) *rlp.Fields {
	if ptr {
		return &rlp.Fields{
			Required: []any{
				&eth.ParentHash, &eth.UncleHash, &eth.Coinbase, &eth.Root, &eth.TxHash, &eth.ReceiptHash,
				&eth.Bloom, &eth.Difficulty, &eth.Number, &eth.GasLimit, &eth.GasUsed, &eth.Time, &eth.Extra,
				&eth.MixDigest, &eth.Nonce, &h.ExtDataHash,
			},
			Optional: []any{
				&eth.BaseFee, &h.ExtDataGasUsed, &h.BlockGasCost,
				&eth.BlobGasUsed, &eth.ExcessBlobGas, &eth.ParentBeaconRoot,
				&h.TimeMilliseconds, &h.MinDelayExcess, &h.TargetExponent, &h.MinPriceExponent,
				&h.SettledHeight, &h.SettledGasUnix, &h.SettledGasNumerator, &h.SettledExcess,
			},
		}
	}
	return &rlp.Fields{
		Required: []any{
			eth.ParentHash, eth.UncleHash, eth.Coinbase, eth.Root, eth.TxHash, eth.ReceiptHash,
			eth.Bloom, eth.Difficulty, eth.Number, eth.GasLimit, eth.GasUsed, eth.Time, eth.Extra,
			eth.MixDigest, eth.Nonce, h.ExtDataHash,
		},
		Optional: []any{
			eth.BaseFee, h.ExtDataGasUsed, h.BlockGasCost,
			eth.BlobGasUsed, eth.ExcessBlobGas, eth.ParentBeaconRoot,
			h.TimeMilliseconds, h.MinDelayExcess, h.TargetExponent, h.MinPriceExponent,
			h.SettledHeight, h.SettledGasUnix, h.SettledGasNumerator, h.SettledExcess,
		},
	}
}

// EncodeRLP implements [ethtypes.HeaderHooks].
func (h *HeaderExtra) EncodeRLP(eth *ethtypes.Header, w io.Writer) error {
	return h.rlpFields(eth, false).EncodeRLP(w)
}

var errTooFewHeaderFields = errors.New("rlp: too few elements for header")

// DecodeRLP implements [ethtypes.HeaderHooks].
func (h *HeaderExtra) DecodeRLP(eth *ethtypes.Header, s *rlp.Stream) error {
	// An EOL here is the enclosing list (e.g. uncles) running out of headers
	// and MUST propagate unchanged so the slice decoder stops.
	if _, _, err := s.Kind(); err != nil {
		return err
	}
	err := h.rlpFields(eth, true).DecodeRLP(s)
	if errors.Is(err, rlp.EOL) {
		// An EOL from inside the header's own list means too few fields. It
		// MUST NOT escape as EOL, or a slice decoder reading this header as an
		// element would take it for the end of its own list.
		return errTooFewHeaderFields
	}
	return err
}

// headerExtraJSON is the JSON form of [HeaderExtra]. Its keys match
// [HeaderExtra.PostRPCMarshal], which produces the encoding.
type headerExtraJSON struct {
	ExtDataHash         common.Hash     `json:"extDataHash"`
	ExtDataGasUsed      *hexutil.Big    `json:"extDataGasUsed"`
	BlockGasCost        *hexutil.Big    `json:"blockGasCost"`
	TimeMilliseconds    *hexutil.Uint64 `json:"timestampMilliseconds"`
	MinDelayExcess      *hexutil.Uint64 `json:"minDelayExcess"`
	TargetExponent      *hexutil.Uint64 `json:"targetExponent"`
	MinPriceExponent    *hexutil.Uint64 `json:"minPriceExponent"`
	SettledHeight       *hexutil.Uint64 `json:"settledHeight"`
	SettledGasUnix      *hexutil.Uint64 `json:"settledGasUnix"`
	SettledGasNumerator *hexutil.Uint64 `json:"settledGasNumerator"`
	SettledExcess       *hexutil.Uint64 `json:"settledExcess"`
}

// EncodeJSON implements [ethtypes.HeaderHooks]: the upstream header JSON,
// including its "hash", with the extra fields added alongside.
func (h *HeaderExtra) EncodeJSON(eth *ethtypes.Header) ([]byte, error) {
	upstream, err := new(ethtypes.NOOPHeaderHooks).EncodeJSON(eth)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(upstream, &m); err != nil {
		return nil, err
	}
	h.PostRPCMarshal(eth, m)
	return json.Marshal(m)
}

// DecodeJSON implements [ethtypes.HeaderHooks].
func (h *HeaderExtra) DecodeJSON(eth *ethtypes.Header, b []byte) error {
	if err := new(ethtypes.NOOPHeaderHooks).DecodeJSON(eth, b); err != nil {
		return err
	}
	var x headerExtraJSON
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	*h = HeaderExtra{
		ExtDataHash:         x.ExtDataHash,
		ExtDataGasUsed:      (*big.Int)(x.ExtDataGasUsed),
		BlockGasCost:        (*big.Int)(x.BlockGasCost),
		TimeMilliseconds:    (*uint64)(x.TimeMilliseconds),
		MinDelayExcess:      (*acp226.DelayExcess)(x.MinDelayExcess),
		TargetExponent:      (*dynamic.TargetExponent)(x.TargetExponent),
		MinPriceExponent:    (*dynamic.PriceExponent)(x.MinPriceExponent),
		SettledHeight:       (*uint64)(x.SettledHeight),
		SettledGasUnix:      (*uint64)(x.SettledGasUnix),
		SettledGasNumerator: (*uint64)(x.SettledGasNumerator),
		SettledExcess:       (*uint64)(x.SettledExcess),
	}
	return nil
}

// PostCopy implements [ethtypes.HeaderHooks]: it deep-copies the extra into
// the copied header.
func (h *HeaderExtra) PostCopy(dst *ethtypes.Header) {
	cp := *h
	if h.ExtDataGasUsed != nil {
		cp.ExtDataGasUsed = new(big.Int).Set(h.ExtDataGasUsed)
	}
	if h.BlockGasCost != nil {
		cp.BlockGasCost = new(big.Int).Set(h.BlockGasCost)
	}
	cp.TimeMilliseconds = copyPtr(h.TimeMilliseconds)
	cp.MinDelayExcess = copyPtr(h.MinDelayExcess)
	cp.TargetExponent = copyPtr(h.TargetExponent)
	cp.MinPriceExponent = copyPtr(h.MinPriceExponent)
	cp.SettledHeight = copyPtr(h.SettledHeight)
	cp.SettledGasUnix = copyPtr(h.SettledGasUnix)
	cp.SettledGasNumerator = copyPtr(h.SettledGasNumerator)
	cp.SettledExcess = copyPtr(h.SettledExcess)
	WithHeaderExtra(dst, &cp)
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// PostRPCMarshal implements [ethtypes.HeaderHooks]: it adds the extra fields
// that are set to the RPC representation of the header.
func (h *HeaderExtra) PostRPCMarshal(_ *ethtypes.Header, m map[string]any) {
	m["extDataHash"] = h.ExtDataHash
	if h.ExtDataGasUsed != nil {
		m["extDataGasUsed"] = (*hexutil.Big)(h.ExtDataGasUsed)
	}
	if h.BlockGasCost != nil {
		m["blockGasCost"] = (*hexutil.Big)(h.BlockGasCost)
	}
	for key, v := range map[string]*uint64{
		"timestampMilliseconds": h.TimeMilliseconds,
		"minDelayExcess":        (*uint64)(h.MinDelayExcess),
		"targetExponent":        (*uint64)(h.TargetExponent),
		"minPriceExponent":      (*uint64)(h.MinPriceExponent),
		"settledHeight":         h.SettledHeight,
		"settledGasUnix":        h.SettledGasUnix,
		"settledGasNumerator":   h.SettledGasNumerator,
		"settledExcess":         h.SettledExcess,
	} {
		if v != nil {
			m[key] = hexutil.Uint64(*v)
		}
	}
}
