// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
)

var (
	errBadCodec   = errors.New("wrong or unknown codec used")
	errExtraSpace = errors.New("trailing buffer space")
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

// Codec is used to serialize and de-serialize transaction objects
type Codec struct{}

/*
 ******************************************************************************
 *********************************** Genesis **********************************
 ******************************************************************************
 */

/* Genesis:
 * Accounts | ? Bytes
 */

// MarshalGenesis returns the byte representation of the genesis
func (c *Codec) MarshalGenesis(accounts []Account) ([]byte, error) {
	return c.MarshalAccounts(accounts)
}

// UnmarshalGenesis attempts to parse the genesis
func (c *Codec) UnmarshalGenesis(b []byte) ([]Account, error) {
	return c.UnmarshalAccounts(b)
}

/*
 ******************************************************************************
 ************************************ Block ***********************************
 ******************************************************************************
 */

/* Block:
 * Codec     | 04 Bytes
 * ParentID  | 32 Bytes
 * NumTxs    | 04 bytes
 * Repeated (NumTxs):
 *     Tx    | ? bytes
 */

const baseOpSize = 40

// MarshalBlock returns the byte representation of the block
func (c *Codec) MarshalBlock(block *Block) ([]byte, error) {
	p := wrappers.Packer{Bytes: make([]byte, baseOpSize+signedTxSize*len(block.txs))}

	c.marshalBlock(block, &p)

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	return p.Bytes, p.Err
}

func (c *Codec) marshalBlock(block *Block, p *wrappers.Packer) {
	if block == nil {
		p.Add(errNil)
		return
	}

	p.PackInt(uint32(CustomID))
	p.PackFixedBytes(block.parentID.Bytes())
	p.PackInt(uint32(len(block.txs)))
	for _, tx := range block.txs {
		if tx != nil {
			p.PackFixedBytes(tx.Bytes())
		} else {
			p.Add(errNil)
			return
		}
	}
}

// UnmarshalBlock attempts to parse an block from a byte array
func (c *Codec) UnmarshalBlock(b []byte) (*Block, error) {
	p := wrappers.Packer{Bytes: b}

	block := c.unmarshalBlock(&p)

	if p.Offset != len(b) {
		p.Add(errExtraSpace)
	}

	return block, p.Err
}

func (c *Codec) unmarshalBlock(p *wrappers.Packer) *Block {
	start := p.Offset

	if codecID := CodecID(p.UnpackInt()); codecID != CustomID {
		p.Add(errBadCodec)
	}

	parentID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))

	txs := []*Tx(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		txs = append(txs, c.unmarshalTx(p))
	}

	if p.Errored() {
		return nil
	}

	bytes := p.Bytes[start:p.Offset]
	return &Block{
		id:       ids.NewID(hashing.ComputeHash256Array(bytes)),
		parentID: parentID,
		txs:      txs,
		bytes:    bytes,
	}
}

/*
 ******************************************************************************
 ************************************* Tx *************************************
 ******************************************************************************
 */

/* Unsigned Tx:
 * Codec       | 04 Bytes
 * Network ID  | 04 bytes
 * Chain ID   | 32 bytes
 * Nonce       | 08 bytes
 * Amount      | 08 bytes
 * Destination | 20 bytes
 */
const unsignedTxSize = 2*wrappers.IntLen + 2*wrappers.LongLen + hashing.AddrLen + hashing.HashLen

/* Tx:
 * Unsigned Tx | 76 bytes
 * Signature   | 65 bytes
 */
const signedTxSize = unsignedTxSize + crypto.SECP256K1RSigLen

// MarshalUnsignedTx returns the byte representation of the unsigned tx
func (c *Codec) MarshalUnsignedTx(tx *Tx) ([]byte, error) {
	p := wrappers.Packer{Bytes: make([]byte, unsignedTxSize)}

	c.marshalUnsignedTx(tx, &p)

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	return p.Bytes, p.Err
}

// MarshalTx returns the byte representation of the tx
func (c *Codec) MarshalTx(tx *Tx) ([]byte, error) {
	p := wrappers.Packer{Bytes: make([]byte, signedTxSize)}

	c.marshalTx(tx, &p)

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	return p.Bytes, p.Err
}

func (c *Codec) marshalUnsignedTx(tx *Tx, p *wrappers.Packer) {
	if tx == nil {
		p.Add(errNil)
		return
	}

	p.PackInt(uint32(CustomID))
	p.PackInt(tx.networkID)
	p.PackFixedBytes(tx.chainID.Bytes())
	p.PackLong(tx.nonce)
	p.PackLong(tx.amount)
	p.PackFixedBytes(tx.to.Bytes())
}

func (c *Codec) marshalTx(tx *Tx, p *wrappers.Packer) {
	c.marshalUnsignedTx(tx, p)

	p.PackFixedBytes(tx.sig)
}

// UnmarshalTx attempts to convert the stream of bytes into a representation
// of a tx
func (c *Codec) UnmarshalTx(b []byte) (*Tx, error) {
	p := wrappers.Packer{Bytes: b}

	tx := c.unmarshalTx(&p)

	if p.Offset != len(b) {
		p.Add(errExtraSpace)
	}

	return tx, p.Err
}

func (c *Codec) unmarshalTx(p *wrappers.Packer) *Tx {
	start := p.Offset

	if codecID := CodecID(p.UnpackInt()); codecID != CustomID {
		p.Add(errBadCodec)
	}

	networkID := p.UnpackInt()
	chainID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
	nonce := p.UnpackLong()
	amount := p.UnpackLong()
	destination, _ := ids.ToShortID(p.UnpackFixedBytes(hashing.AddrLen))
	sig := p.UnpackFixedBytes(crypto.SECP256K1RSigLen)

	if p.Errored() {
		return nil
	}

	bytes := p.Bytes[start:p.Offset]
	return &Tx{
		id:           ids.NewID(hashing.ComputeHash256Array(bytes)),
		networkID:    networkID,
		chainID:      chainID,
		nonce:        nonce,
		amount:       amount,
		to:           destination,
		sig:          sig,
		bytes:        bytes,
		verification: make(chan error, 1),
	}
}

/*
 ******************************************************************************
 ********************************** Accounts **********************************
 ******************************************************************************
 */

/* Accounts:
 * NumAccounts | 04 Bytes
 * Repeated (NumAccounts):
 *     Account | 36 bytes
 */

const baseAccountsSize = 4

// MarshalAccounts returns the byte representation of a list of accounts
func (c *Codec) MarshalAccounts(accounts []Account) ([]byte, error) {
	p := wrappers.Packer{Bytes: make([]byte, baseAccountsSize+accountSize*len(accounts))}

	c.marshalAccounts(accounts, &p)

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	return p.Bytes, p.Err
}

func (c *Codec) marshalAccounts(accounts []Account, p *wrappers.Packer) {
	p.PackInt(uint32(len(accounts)))
	for _, account := range accounts {
		c.marshalAccount(account, p)
	}
}

// UnmarshalAccounts attempts to parse a list of accounts from a byte array
func (c *Codec) UnmarshalAccounts(b []byte) ([]Account, error) {
	p := wrappers.Packer{Bytes: b}

	account := c.unmarshalAccounts(&p)

	if p.Offset != len(b) {
		p.Add(errExtraSpace)
	}

	return account, p.Err
}

func (c *Codec) unmarshalAccounts(p *wrappers.Packer) []Account {
	accounts := []Account(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		accounts = append(accounts, c.unmarshalAccount(p))
	}
	return accounts
}

/*
 ******************************************************************************
 *********************************** Account **********************************
 ******************************************************************************
 */

/* Account:
 * ID        | 20 bytes
 * Nonce     | 08 bytes
 * Balance   | 08 bytes
 */

const accountSize = 36

// MarshalAccount returns the byte representation of the account
func (c *Codec) MarshalAccount(account Account) ([]byte, error) {
	p := wrappers.Packer{Bytes: make([]byte, accountSize)}

	c.marshalAccount(account, &p)

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	return p.Bytes, p.Err
}

func (c *Codec) marshalAccount(account Account, p *wrappers.Packer) {
	p.PackFixedBytes(account.id.Bytes())
	p.PackLong(account.nonce)
	p.PackLong(account.balance)
}

// UnmarshalAccount attempts to parse an account from a byte array
func (c *Codec) UnmarshalAccount(b []byte) (Account, error) {
	p := wrappers.Packer{Bytes: b}

	account := c.unmarshalAccount(&p)

	if p.Offset != len(b) {
		p.Add(errExtraSpace)
	}

	return account, p.Err
}

func (c *Codec) unmarshalAccount(p *wrappers.Packer) Account {
	id, _ := ids.ToShortID(p.UnpackFixedBytes(hashing.AddrLen))
	nonce := p.UnpackLong()
	balance := p.UnpackLong()

	return Account{
		id:      id,
		nonce:   nonce,
		balance: balance,
	}
}
