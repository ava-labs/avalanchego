package cblock

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// rpcBlock is eth_getBlockByNumber(n, true) as coreth's RPC returns it.
type rpcBlock struct {
	ParentHash          string   `json:"parentHash"`
	UncleHash           string   `json:"sha3Uncles"`
	Coinbase            string   `json:"miner"`
	Root                string   `json:"stateRoot"`
	TxHash              string   `json:"transactionsRoot"`
	ReceiptHash         string   `json:"receiptsRoot"`
	Bloom               string   `json:"logsBloom"`
	Difficulty          string   `json:"difficulty"`
	Number              string   `json:"number"`
	GasLimit            string   `json:"gasLimit"`
	GasUsed             string   `json:"gasUsed"`
	Time                string   `json:"timestamp"`
	Extra               string   `json:"extraData"`
	MixDigest           string   `json:"mixHash"`
	Nonce               string   `json:"nonce"`
	ExtDataHash         string   `json:"extDataHash"`
	BaseFee             *string  `json:"baseFeePerGas"`
	ExtDataGasUsed      *string  `json:"extDataGasUsed"`
	BlockGasCost        *string  `json:"blockGasCost"`
	BlobGasUsed         *string  `json:"blobGasUsed"`
	ExcessBlobGas       *string  `json:"excessBlobGas"`
	ParentBeaconRoot    *string  `json:"parentBeaconBlockRoot"`
	TimeMilliseconds    *string  `json:"timestampMilliseconds"`
	MinDelayExcess      *string  `json:"minDelayExcess"`
	TargetExponent      *string  `json:"targetExponent"`
	MinPriceExponent    *string  `json:"minPriceExponent"`
	SettledHeight       *string  `json:"settledHeight"`
	SettledGasUnix      *string  `json:"settledGasUnix"`
	SettledGasNumerator *string  `json:"settledGasNumerator"`
	SettledExcess       *string  `json:"settledExcess"`
	Hash                string   `json:"hash"`
	Size                string   `json:"size"`
	ExtData             *string  `json:"blockExtraData"`
	Uncles              []string `json:"uncles"`
	Txs                 []rpcTx  `json:"transactions"`
}

type rpcTx struct {
	Type       string  `json:"type"`
	ChainID    *string `json:"chainId"`
	Nonce      string  `json:"nonce"`
	GasPrice   *string `json:"gasPrice"`
	FeeCap     *string `json:"maxFeePerGas"`
	TipCap     *string `json:"maxPriorityFeePerGas"`
	Gas        string  `json:"gas"`
	To         *string `json:"to"`
	Value      string  `json:"value"`
	Input      string  `json:"input"`
	AccessList *[]struct {
		Address     string   `json:"address"`
		StorageKeys []string `json:"storageKeys"`
	} `json:"accessList"`
	V, R, S string
	YParity *string `json:"yParity"`
	Hash    string  `json:"hash"`
	From    string  `json:"from"`
}

// hx parses hex data and fails on malformed input.
type hx struct{ err error }

func (p *hx) data(s string) []byte {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil && p.err == nil {
		p.err = fmt.Errorf("hex %q: %w", s, err)
	}
	return b
}

func (p *hx) fixed(dst []byte, s string) {
	b := p.data(s)
	if len(b) != len(dst) && p.err == nil {
		p.err = fmt.Errorf("hex %q: want %d bytes", s, len(dst))
	}
	copy(dst, b)
}

func (p *hx) big(s string) *big.Int {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if (!ok || !strings.HasPrefix(s, "0x")) && p.err == nil {
		p.err = fmt.Errorf("quantity %q", s)
		return new(big.Int)
	}
	return v
}

func (p *hx) u256(s string) (v U256) {
	b := p.big(s)
	if b.BitLen() > 256 && p.err == nil {
		p.err = fmt.Errorf("quantity %q over 256 bits", s)
	}
	b.FillBytes(v[:])
	return v
}

func (p *hx) u64(s string) uint64 {
	b := p.big(s)
	if !b.IsUint64() && p.err == nil {
		p.err = fmt.Errorf("quantity %q over 64 bits", s)
	}
	return b.Uint64()
}

// req returns a required JSON field.
func (p *hx) req(s *string, name string) string {
	if s == nil {
		if p.err == nil {
			p.err = fmt.Errorf("rpc: missing %s", name)
		}
		return "0x0"
	}
	return *s
}

func (p *hx) optU256(s *string) *U256 {
	if s == nil {
		return nil
	}
	v := p.u256(*s)
	return &v
}

func (p *hx) optU64(s *string) *uint64 {
	if s == nil {
		return nil
	}
	v := p.u64(*s)
	return &v
}

// FromRPC builds the exact coreth block from an eth_getBlockByNumber(n, true) result of
// coreth's RPC (every header field including the SAE ones, the signed txs, blockExtraData as
// ExtData, Version 0: coreth never sets it and SAE VerifyBlockSyntax rejects anything else).
// It verifies nothing: the caller compares Hash() with the RPC "hash", Tx.Hash() with each
// tx "hash", and may compare len(Encode()) with "size". A block with uncles is an error
// (the RPC gives only their hashes; coreth and SAE blocks have none).
func FromRPC(blockJSON []byte) (*Block, error) {
	b, _, err := fromRPC(blockJSON)
	return b, err
}

// fromRPC is FromRPC that also returns the parsed JSON.
func fromRPC(js []byte) (*Block, *rpcBlock, error) {
	var r rpcBlock
	if err := json.Unmarshal(js, &r); err != nil {
		return nil, nil, err
	}
	if len(r.Uncles) != 0 {
		return nil, nil, fmt.Errorf("block %s has %d uncles; the RPC gives only their hashes", r.Number, len(r.Uncles))
	}
	p := &hx{}
	b := &Block{}
	h := &b.Header
	p.fixed(h.ParentHash[:], r.ParentHash)
	p.fixed(h.UncleHash[:], r.UncleHash)
	p.fixed(h.Coinbase[:], r.Coinbase)
	p.fixed(h.Root[:], r.Root)
	p.fixed(h.TxHash[:], r.TxHash)
	p.fixed(h.ReceiptHash[:], r.ReceiptHash)
	p.fixed(h.Bloom[:], r.Bloom)
	h.Difficulty = p.u256(r.Difficulty)
	h.Number = p.u64(r.Number)
	h.GasLimit = p.u64(r.GasLimit)
	h.GasUsed = p.u64(r.GasUsed)
	h.Time = p.u64(r.Time)
	h.Extra = p.data(r.Extra)
	p.fixed(h.MixDigest[:], r.MixDigest)
	p.fixed(h.Nonce[:], r.Nonce)
	p.fixed(h.ExtDataHash[:], r.ExtDataHash)
	h.BaseFee = p.optU256(r.BaseFee)
	h.ExtDataGasUsed = p.optU256(r.ExtDataGasUsed)
	h.BlockGasCost = p.optU256(r.BlockGasCost)
	h.BlobGasUsed = p.optU64(r.BlobGasUsed)
	h.ExcessBlobGas = p.optU64(r.ExcessBlobGas)
	if r.ParentBeaconRoot != nil {
		h.ParentBeaconRoot = new([32]byte)
		p.fixed(h.ParentBeaconRoot[:], *r.ParentBeaconRoot)
	}
	h.TimeMilliseconds = p.optU64(r.TimeMilliseconds)
	h.MinDelayExcess = p.optU64(r.MinDelayExcess)
	h.TargetExponent = p.optU64(r.TargetExponent)
	h.MinPriceExponent = p.optU64(r.MinPriceExponent)
	h.SettledHeight = p.optU64(r.SettledHeight)
	h.SettledGasUnix = p.optU64(r.SettledGasUnix)
	h.SettledGasNumerator = p.optU64(r.SettledGasNumerator)
	h.SettledExcess = p.optU64(r.SettledExcess)
	if r.ExtData != nil {
		b.ExtData = p.data(*r.ExtData)
	}
	for i := range r.Txs {
		j := &r.Txs[i]
		t := &Tx{Type: byte(p.u64(j.Type))}
		t.Nonce = p.u64(j.Nonce)
		t.Gas = p.u64(j.Gas)
		t.Value = p.u256(j.Value)
		t.Data = p.data(j.Input)
		if j.To != nil {
			t.To = new([20]byte)
			p.fixed(t.To[:], *j.To)
		}
		t.V, t.R, t.S = p.u256(j.V), p.u256(j.R), p.u256(j.S)
		switch t.Type {
		case LegacyTxType:
			t.GasPrice = p.u256(p.req(j.GasPrice, "gasPrice"))
		case AccessListTxType:
			t.GasPrice = p.u256(p.req(j.GasPrice, "gasPrice"))
			t.ChainID = p.u256(p.req(j.ChainID, "chainId"))
		case DynamicFeeTxType:
			t.GasTipCap = p.u256(p.req(j.TipCap, "maxPriorityFeePerGas"))
			t.GasFeeCap = p.u256(p.req(j.FeeCap, "maxFeePerGas"))
			t.ChainID = p.u256(p.req(j.ChainID, "chainId"))
		default:
			return nil, nil, fmt.Errorf("block %s tx %s: type %d", r.Number, j.Hash, t.Type)
		}
		if t.Type != LegacyTxType {
			if j.AccessList == nil {
				return nil, nil, fmt.Errorf("tx %s: no accessList", j.Hash)
			}
			for _, e := range *j.AccessList {
				var a AccessTuple
				p.fixed(a.Address[:], e.Address)
				for _, k := range e.StorageKeys {
					var kk [32]byte
					p.fixed(kk[:], k)
					a.StorageKeys = append(a.StorageKeys, kk)
				}
				t.AccessList = append(t.AccessList, a)
			}
		}
		t.Encode()
		b.Txs = append(b.Txs, t)
	}
	return b, &r, p.err
}
