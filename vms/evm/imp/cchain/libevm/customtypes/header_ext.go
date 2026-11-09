// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/dynamic"

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
// every historical C-Chain block hash still verifies. See README.md for how to
// add a field.
type HeaderExtra struct {
	ExtDataHash      common.Hash             `json:"extDataHash"`
	ExtDataGasUsed   *big.Int                `json:"extDataGasUsed"`
	BlockGasCost     *big.Int                `json:"blockGasCost"`
	TimeMilliseconds *uint64                 `json:"timestampMilliseconds"`
	MinDelayExcess   *dynamic.DelayExponent  `json:"minDelayExcess"`
	TargetExponent   *dynamic.TargetExponent `json:"targetExponent"`
	MinPriceExponent *dynamic.PriceExponent  `json:"minPriceExponent"`

	SettledHeight       *uint64 `json:"settledHeight"`
	SettledGasUnix      *uint64 `json:"settledGasUnix"`
	SettledGasNumerator *uint64 `json:"settledGasNumerator"`
	SettledExcess       *uint64 `json:"settledExcess"`

	predicateResults atomic.Pointer[predicate.BlockResults]
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
// The fields are pointers so that the list also serves decoding.
func (h *HeaderExtra) rlpFields(eth *ethtypes.Header) headerRLPFields {
	return headerRLPFields{
		Required: [...]any{
			&eth.ParentHash, &eth.UncleHash, &eth.Coinbase, &eth.Root, &eth.TxHash, &eth.ReceiptHash,
			&eth.Bloom, &eth.Difficulty, &eth.Number, &eth.GasLimit, &eth.GasUsed, &eth.Time, &eth.Extra,
			&eth.MixDigest, &eth.Nonce, &h.ExtDataHash,
		},
		Optional: [...]any{
			&eth.BaseFee, &h.ExtDataGasUsed, &h.BlockGasCost,
			&eth.BlobGasUsed, &eth.ExcessBlobGas, &eth.ParentBeaconRoot,
			&h.TimeMilliseconds, &h.MinDelayExcess, &h.TargetExponent, &h.MinPriceExponent,
			&h.SettledHeight, &h.SettledGasUnix, &h.SettledGasNumerator, &h.SettledExcess,
		},
	}
}

// headerRLPFields holds arrays rather than slices so that building it does
// not allocate.
type headerRLPFields struct {
	Required [16]any
	Optional [14]any
}

// EncodeRLP implements [ethtypes.HeaderHooks]. It writes the fields directly
// rather than through [rlp.Fields], which encodes each one by reflection.
func (h *HeaderExtra) EncodeRLP(eth *ethtypes.Header, w io.Writer) error {
	f := h.rlpFields(eth)
	last := -1
	for i, p := range f.Optional {
		if !reflect.ValueOf(p).Elem().IsNil() {
			last = i
		}
	}
	b := rlp.NewEncoderBuffer(w)
	list := b.List()
	for _, p := range f.Required {
		writeRLPField(b, p)
	}
	for _, p := range f.Optional[:last+1] {
		writeRLPField(b, p)
	}
	b.ListEnd(list)
	return b.Flush()
}

// writeRLPField writes the field `p` points to. A nil optional field is
// written as an empty string.
func writeRLPField(b rlp.EncoderBuffer, p any) {
	switch p := p.(type) {
	case *common.Hash:
		b.WriteBytes(p[:])
	case *common.Address:
		b.WriteBytes(p[:])
	case *ethtypes.Bloom:
		b.WriteBytes(p[:])
	case *ethtypes.BlockNonce:
		b.WriteBytes(p[:])
	case *[]byte:
		b.WriteBytes(*p)
	case *uint64:
		b.WriteUint64(*p)
	case **big.Int:
		if *p == nil {
			b.WriteBytes(nil)
		} else {
			b.WriteBigInt(*p)
		}
	case **common.Hash:
		if *p == nil {
			b.WriteBytes(nil)
		} else {
			b.WriteBytes((*p)[:])
		}
	case **uint64:
		writeOptionalUint(b, p)
	case **dynamic.DelayExponent:
		writeOptionalUint(b, p)
	case **dynamic.TargetExponent:
		writeOptionalUint(b, p)
	case **dynamic.PriceExponent:
		writeOptionalUint(b, p)
	default:
		panic(fmt.Sprintf("unexpected header field type %T", p))
	}
}

func writeOptionalUint[T ~uint64](b rlp.EncoderBuffer, p **T) {
	if *p == nil {
		b.WriteBytes(nil)
	} else {
		b.WriteUint64(uint64(**p))
	}
}

var errTooFewHeaderFields = errors.New("rlp: too few elements for header")

// DecodeRLP implements [ethtypes.HeaderHooks].
func (h *HeaderExtra) DecodeRLP(eth *ethtypes.Header, s *rlp.Stream) error {
	// An EOL here is the enclosing list (e.g. uncles) running out of headers
	// and MUST propagate unchanged so the slice decoder stops.
	if _, _, err := s.Kind(); err != nil {
		return err
	}
	f := h.rlpFields(eth)
	err := f.decode(s)
	if errors.Is(err, rlp.EOL) {
		// An EOL from inside the header's own list means too few fields. It
		// MUST NOT escape as EOL, or a slice decoder reading this header as an
		// element would take it for the end of its own list.
		return errTooFewHeaderFields
	}
	return err
}

func (f *headerRLPFields) decode(s *rlp.Stream) error {
	if _, err := s.List(); err != nil {
		return err
	}
	for _, p := range f.Required {
		if err := readRLPField(s, p); err != nil {
			return err
		}
	}
	for _, p := range f.Optional {
		if !s.MoreDataInList() {
			break
		}
		if err := readRLPField(s, p); err != nil {
			return err
		}
	}
	return s.ListEnd()
}

// readRLPField decodes the next item into the field `p` points to, without
// [rlp.Stream.Decode]'s per-call type lookup.
func readRLPField(s *rlp.Stream, p any) error {
	switch p := p.(type) {
	case *common.Hash:
		return s.ReadBytes(p[:])
	case *common.Address:
		return s.ReadBytes(p[:])
	case *ethtypes.Bloom:
		return s.ReadBytes(p[:])
	case *ethtypes.BlockNonce:
		return s.ReadBytes(p[:])
	case *[]byte:
		v, err := s.Bytes()
		if err != nil {
			return err
		}
		*p = v
	case *uint64:
		v, err := s.Uint64()
		if err != nil {
			return err
		}
		*p = v
	case **big.Int:
		v, err := s.BigInt()
		if err != nil {
			return err
		}
		if *p == nil {
			*p = v
		} else {
			(*p).Set(v)
		}
	case **common.Hash:
		v := *p
		if v == nil {
			v = new(common.Hash)
		}
		if err := s.ReadBytes(v[:]); err != nil {
			return err
		}
		*p = v
	case **uint64:
		return readOptionalUint(s, p)
	case **dynamic.DelayExponent:
		return readOptionalUint(s, p)
	case **dynamic.TargetExponent:
		return readOptionalUint(s, p)
	case **dynamic.PriceExponent:
		return readOptionalUint(s, p)
	default:
		panic(fmt.Sprintf("unexpected header field type %T", p))
	}
	return nil
}

func readOptionalUint[T ~uint64](s *rlp.Stream, p **T) error {
	v, err := s.Uint64()
	if err != nil {
		return err
	}
	if *p == nil {
		*p = new(T)
	}
	**p = T(v)
	return nil
}

// EncodeJSON implements [ethtypes.HeaderHooks]: the upstream header JSON,
// including its "hash", with the extra fields added alongside.
func (h *HeaderExtra) EncodeJSON(eth *ethtypes.Header) ([]byte, error) {
	upstream, err := new(ethtypes.NOOPHeaderHooks).EncodeJSON(eth)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any)
	h.PostRPCMarshal(eth, m)
	extra, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	// Splice the two objects: `{a}` and `{b}` become `{a,b}`. Neither is empty,
	// since the upstream header always has fields and ExtDataHash is always set.
	return append(append(upstream[:len(upstream)-1], ','), extra[1:]...), nil
}

// DecodeJSON implements [ethtypes.HeaderHooks].
func (h *HeaderExtra) DecodeJSON(eth *ethtypes.Header, b []byte) error {
	if err := new(ethtypes.NOOPHeaderHooks).DecodeJSON(eth, b); err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*h = HeaderExtra{}
	v := reflect.ValueOf(h).Elem()
	for i := range v.NumField() {
		f, key := v.Field(i), v.Type().Field(i).Tag.Get("json")
		raw, ok := m[key]
		if key == "" || !ok || string(raw) == "null" {
			continue
		}
		var err error
		switch p := f.Addr().Interface().(type) {
		case **big.Int:
			*p = new(big.Int)
			err = json.Unmarshal(raw, (*hexutil.Big)(*p))
		default:
			if f.Kind() != reflect.Pointer {
				err = json.Unmarshal(raw, p)
				break
			}
			var u hexutil.Uint64
			err = json.Unmarshal(raw, &u)
			f.Set(reflect.New(f.Type().Elem()))
			f.Elem().SetUint(uint64(u))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// PostCopy implements [ethtypes.HeaderHooks]: it deep-copies the extra into
// the copied header.
func (h *HeaderExtra) PostCopy(dst *ethtypes.Header) {
	cp := new(HeaderExtra)
	src, out := reflect.ValueOf(h).Elem(), reflect.ValueOf(cp).Elem()
	for i := range src.NumField() {
		f := src.Field(i)
		switch {
		case !src.Type().Field(i).IsExported():
		case f.Kind() != reflect.Pointer || f.IsNil():
			out.Field(i).Set(f)
		default:
			c := reflect.New(f.Type().Elem())
			if b, ok := f.Interface().(*big.Int); ok {
				c.Interface().(*big.Int).Set(b)
			} else {
				c.Elem().Set(f.Elem())
			}
			out.Field(i).Set(c)
		}
	}
	WithHeaderExtra(dst, cp)
}

// PostRPCMarshal implements [ethtypes.HeaderHooks]: it adds the extra fields
// that are set to the RPC representation of the header.
func (h *HeaderExtra) PostRPCMarshal(_ *ethtypes.Header, m map[string]any) {
	v := reflect.ValueOf(h).Elem()
	for i := range v.NumField() {
		f, key := v.Field(i), v.Type().Field(i).Tag.Get("json")
		switch {
		case key == "":
		case f.Kind() != reflect.Pointer:
			m[key] = f.Interface()
		case f.IsNil():
		case f.Type() == reflect.TypeFor[*big.Int]():
			m[key] = (*hexutil.Big)(f.Interface().(*big.Int))
		default:
			m[key] = hexutil.Uint64(f.Elem().Uint())
		}
	}
}
