// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
)

var (
	errBadCodec   = errors.New("wrong or unknown codec used")
	errExtraSpace = errors.New("trailing buffer space")
	errOutputType = errors.New("unknown output type")
	errInputType  = errors.New("unknown input type")
	errNil        = errors.New("nil value is invalid")
)

// CodecID is an identifier for a codec
type CodecID uint32

// Codec types
const (
	NoID CodecID = iota
	GenericID
	CustomID
	// TODO: Utilize a standard serialization library. Must have a canonical
	// serialization format.
)

// Verify that the codec is a known codec value. Returns nil if the codec is
// valid.
func (c CodecID) Verify() error {
	switch c {
	case NoID, GenericID, CustomID:
		return nil
	default:
		return errBadCodec
	}
}

func (c CodecID) String() string {
	switch c {
	case NoID:
		return "No Codec"
	case GenericID:
		return "Generic Codec"
	case CustomID:
		return "Custom Codec"
	default:
		return "Unknown Codec"
	}
}

// MaxSize is the maximum allowed tx size. It is necessary to deter DoS.
const MaxSize = 1 << 18

// Output types
const (
	OutputPaymentID uint32 = iota
	OutputTakeOrLeaveID
)

// Input types
const (
	InputID uint32 = iota
)

// Codec is used to serialize and de-serialize transaction objects
type Codec struct{}

/*
 ******************************************************************************
 ************************************* Tx *************************************
 ******************************************************************************
 */

/* Unsigned Tx:
 * Codec      | 04 bytes
 * Network ID | 04 bytes
 * Chain ID  | 32 bytes
 * NumOuts    | 04 bytes
 * Repeated (NumOuts):
 *     Out    | ? bytes
 * NumIns     | 04 bytes
 * Repeated (NumIns):
 *     In     | ? bytes
 */

/* Tx:
 * Unsigned Tx | ? bytes
 * Repeated (NumIns):
 *     Sig     | ? bytes
 */

// MarshalUnsignedTx returns the byte representation of the unsigned tx
func (c *Codec) MarshalUnsignedTx(tx *Tx) ([]byte, error) {
	p := wrappers.Packer{MaxSize: MaxSize}

	c.marshalUnsignedTx(tx, &p)

	return p.Bytes, p.Err
}

// MarshalTx returns the byte representation of the tx
func (c *Codec) MarshalTx(tx *Tx) ([]byte, error) {
	p := wrappers.Packer{MaxSize: MaxSize}

	c.marshalUnsignedTx(tx, &p)

	if tx != nil {
		for _, in := range tx.ins {
			c.marshalSigs(in, &p)
		}
	}

	return p.Bytes, p.Err
}

func (c *Codec) marshalUnsignedTx(tx *Tx, p *wrappers.Packer) {
	if tx == nil {
		p.Add(fmt.Errorf("serialization error occurred, Error:%w, Index=%d", errNil, p.Offset))
		return
	}

	p.PackInt(uint32(CustomID))
	p.PackInt(tx.networkID)
	p.PackFixedBytes(tx.chainID.Bytes())

	outs := tx.outs
	p.PackInt(uint32(len(outs)))
	for _, out := range outs {
		c.marshalOutput(out, p)
	}

	ins := tx.ins
	p.PackInt(uint32(len(ins)))
	for _, in := range ins {
		c.marshalInput(in, p)
	}
}

// UnmarshalTx attempts to convert the stream of bytes into a representation
// of a tx
func (c *Codec) UnmarshalTx(b []byte) (*Tx, error) {
	p := wrappers.Packer{Bytes: b}

	tx := c.unmarshalTx(&p)

	if p.Offset != len(b) {
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errExtraSpace, p.Offset))
	}

	return tx, p.Err
}

func (c *Codec) unmarshalTx(p *wrappers.Packer) *Tx {
	start := p.Offset

	if codecID := CodecID(p.UnpackInt()); codecID != CustomID {
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errBadCodec, p.Offset))
	}

	networkID := p.UnpackInt()
	chainID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))

	outs := []Output(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		outs = append(outs, c.unmarshalOutput(p))
	}

	ins := []Input(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		ins = append(ins, c.unmarshalInput(p))
	}

	for _, in := range ins {
		c.unmarshalSigs(in, p)
	}

	if p.Errored() {
		return nil
	}

	bytes := p.Bytes[start:p.Offset]
	return &Tx{
		id:        ids.NewID(hashing.ComputeHash256Array(bytes)),
		networkID: networkID,
		chainID:   chainID,
		ins:       ins,
		outs:      outs,
		bytes:     bytes,
	}
}

/*
 ******************************************************************************
 *********************************** UTXOs ************************************
 ******************************************************************************
 */

/* UTXOs:
 * NumUTXOs | 4 bytes
 * Repeated (NumUTXOs):
 *     UTXO | ? bytes
 */

// MarshalUTXOs returns the byte representation of the utxos
func (c *Codec) MarshalUTXOs(utxos []*UTXO) ([]byte, error) {
	p := wrappers.Packer{MaxSize: MaxSize}

	p.PackInt(uint32(len(utxos)))
	for _, utxo := range utxos {
		if utxo == nil {
			p.Add(fmt.Errorf("serialization error occurred, Error:%w, Index=%d", errNil, p.Offset))
			break
		} else {
			p.PackFixedBytes(utxo.Bytes())
		}
	}

	return p.Bytes, p.Err
}

// UnmarshalUTXOs attempts to convert the stream of bytes into a representation
// of a slice of utxos
func (c *Codec) UnmarshalUTXOs(b []byte) ([]*UTXO, error) {
	p := wrappers.Packer{Bytes: b}

	utxos := []*UTXO(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		utxos = append(utxos, c.unmarshalUTXO(&p))
	}

	if p.Offset != len(b) {
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errExtraSpace, p.Offset))
	}
	return utxos, p.Err
}

/*
 ******************************************************************************
 ************************************ UTXO ************************************
 ******************************************************************************
 */

/* UTXO:
 * TxID    | 32 Bytes
 * TxIndex | 04 bytes
 * Output  | ?? bytes
 */

// MarshalUTXO returns the byte representation of the utxo
func (c *Codec) MarshalUTXO(utxo *UTXO) ([]byte, error) {
	p := wrappers.Packer{MaxSize: MaxSize}

	c.marshalUTXO(utxo, &p)

	return p.Bytes, p.Err
}

func (c *Codec) marshalUTXO(utxo *UTXO, p *wrappers.Packer) {
	if utxo == nil {
		p.Add(fmt.Errorf("serialization error occurred, Error:%w, Index=%d", errNil, p.Offset))
		return
	}

	txID, txIndex := utxo.Source()
	p.PackFixedBytes(txID.Bytes())
	p.PackInt(txIndex)
	c.marshalOutput(utxo.Out(), p)
}

// UnmarshalUTXO attempts to convert the stream of bytes into a representation
// of an utxo
func (c *Codec) UnmarshalUTXO(b []byte) (*UTXO, error) {
	p := wrappers.Packer{Bytes: b}

	utxo := c.unmarshalUTXO(&p)

	if p.Offset != len(b) {
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errExtraSpace, p.Offset))
	}

	return utxo, p.Err
}

func (c *Codec) unmarshalUTXO(p *wrappers.Packer) *UTXO {
	start := p.Offset

	sourceID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
	sourceIndex := p.UnpackInt()
	out := c.unmarshalOutput(p)

	return &UTXO{
		sourceID:    sourceID,
		sourceIndex: sourceIndex,
		id:          sourceID.Prefix(uint64(sourceIndex)),
		out:         out,
		bytes:       p.Bytes[start:p.Offset],
	}
}

/*
 ******************************************************************************
 *********************************** Output ***********************************
 ******************************************************************************
 */

/* Output Payment:
 * OutputID  | 04 Bytes
 * Amount    | 08 bytes
 * Locktime  | 08 bytes
 * Threshold | 04 bytes
 * NumAddrs  | 04 bytes
 * Repeated (NumAddrs):
 *     Addr  | 20 bytes
 */

/* Output Take-or-Leave:
 * OutputID      | 04 Bytes
 * Amount        | 08 bytes
 * Locktime      | 08 bytes
 * Threshold     | 04 bytes
 * NumAddrs      | 04 bytes
 * Repeated (NumAddrs):
 *     Addr      | 20 bytes
 * FallLocktime  | 08 bytes
 * FallThreshold | 04 bytes
 * NumFallAddrs  | 04 bytes
 * Repeated (NumFallAddrs):
 *     Addr      | 20 bytes
 */

// MarshalOutput returns the byte representation of the output
func (c *Codec) MarshalOutput(out Output) ([]byte, error) {
	p := wrappers.Packer{MaxSize: MaxSize}

	c.marshalOutput(out, &p)

	return p.Bytes, p.Err
}

func (c *Codec) marshalOutput(out Output, p *wrappers.Packer) {
	switch o := out.(type) {
	case *OutputPayment:
		p.PackInt(OutputPaymentID)
		p.PackLong(o.amount)
		p.PackLong(o.locktime)
		p.PackInt(o.threshold)
		p.PackInt(uint32(len(o.addresses)))
		for _, addr := range o.addresses {
			p.PackFixedBytes(addr.Bytes())
		}
	case *OutputTakeOrLeave:
		p.PackInt(OutputTakeOrLeaveID)
		p.PackLong(o.amount)
		p.PackLong(o.locktime1)
		p.PackInt(o.threshold1)
		p.PackInt(uint32(len(o.addresses1)))
		for _, addr := range o.addresses1 {
			p.PackFixedBytes(addr.Bytes())
		}
		p.PackLong(o.locktime2)
		p.PackInt(o.threshold2)
		p.PackInt(uint32(len(o.addresses2)))
		for _, addr := range o.addresses2 {
			p.PackFixedBytes(addr.Bytes())
		}
	default:
		p.Add(fmt.Errorf("serialization error occurred, Error:%w, Index=%d", errOutputType, p.Offset))
	}
}

func (c *Codec) unmarshalOutput(p *wrappers.Packer) Output {
	switch p.UnpackInt() {
	case OutputPaymentID:
		amount := p.UnpackLong()
		locktime := p.UnpackLong()
		threshold := p.UnpackInt()

		addresses := []ids.ShortID(nil)
		for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
			addr, _ := ids.ToShortID(p.UnpackFixedBytes(hashing.AddrLen))
			addresses = append(addresses, addr)
		}

		if p.Errored() {
			return nil
		}

		return &OutputPayment{
			amount:    amount,
			locktime:  locktime,
			threshold: threshold,
			addresses: addresses,
		}
	case OutputTakeOrLeaveID:
		amount := p.UnpackLong()
		locktime1 := p.UnpackLong()
		threshold1 := p.UnpackInt()

		addresses1 := []ids.ShortID(nil)
		for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
			addr, _ := ids.ToShortID(p.UnpackFixedBytes(hashing.AddrLen))
			addresses1 = append(addresses1, addr)
		}

		locktime2 := p.UnpackLong()
		threshold2 := p.UnpackInt()

		addresses2 := []ids.ShortID(nil)
		for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
			addr, _ := ids.ToShortID(p.UnpackFixedBytes(hashing.AddrLen))
			addresses2 = append(addresses2, addr)
		}

		if p.Errored() {
			return nil
		}

		return &OutputTakeOrLeave{
			amount:     amount,
			locktime1:  locktime1,
			threshold1: threshold1,
			addresses1: addresses1,
			locktime2:  locktime2,
			threshold2: threshold2,
			addresses2: addresses2,
		}
	default:
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errOutputType, p.Offset))
		return nil
	}
}

/*
 ******************************************************************************
 *********************************** Input ************************************
 ******************************************************************************
 */

/* Input:
 * ObjectID | 04 Bytes
 * TxID     | 32 bytes
 * TxIndex  | 04 bytes
 * Amount   | 08 bytes
 * NumSigs  | 04 bytes
 * Repeated (NumSigs):
 *     Sig  | 04 bytes
 */

func (c *Codec) marshalInput(rawInput Input, p *wrappers.Packer) {
	switch in := rawInput.(type) {
	case *InputPayment:
		p.PackInt(InputID)
		p.PackFixedBytes(in.sourceID.Bytes())
		p.PackInt(in.sourceIndex)
		p.PackLong(in.amount)

		p.PackInt(uint32(len(in.sigs)))
		for _, sig := range in.sigs {
			p.PackInt(sig.index)
		}
	default:
		p.Add(fmt.Errorf("serialization error occurred, Error:%w, Index=%d", errInputType, p.Offset))
	}
}

func (c *Codec) unmarshalInput(p *wrappers.Packer) Input {
	switch inputID := p.UnpackInt(); inputID {
	case InputID:
		txID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
		index := p.UnpackInt()
		amount := p.UnpackLong()

		sigs := []*Sig(nil)
		for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
			sigs = append(sigs, &Sig{index: p.UnpackInt()})
		}

		return &InputPayment{
			sourceID:    txID,
			sourceIndex: index,
			amount:      amount,
			sigs:        sigs,
		}
	default:
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errInputType, p.Offset))
		return nil
	}
}

/*
 ******************************************************************************
 ************************************ Sig *************************************
 ******************************************************************************
 */

/* Sig:
 * Repeated (NumSigs):
 *     Sig    | 65 bytes
 */

func (c *Codec) marshalSigs(rawInput Input, p *wrappers.Packer) {
	switch in := rawInput.(type) {
	case *InputPayment:
		for _, sig := range in.sigs {
			p.PackFixedBytes(sig.sig)
		}
	default:
		p.Add(fmt.Errorf("serialization error occurred, Error:%w, Index=%d", errInputType, p.Offset))
	}
}

func (c *Codec) unmarshalSigs(rawInput Input, p *wrappers.Packer) {
	switch in := rawInput.(type) {
	case *InputPayment:
		for _, sig := range in.sigs {
			sig.sig = p.UnpackFixedBytes(crypto.SECP256K1RSigLen)
		}
	default:
		p.Add(fmt.Errorf("parse error occurred, Error:%w, Index=%d", errInputType, p.Offset))
	}
}
