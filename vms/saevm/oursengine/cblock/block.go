// Package cblock decodes C-Chain blocks: the coreth block RLP (header, signed txs, atomic
// ext data) and the proposervm container around it. No geth/libevm/coreth code: the RLP is
// our own, written from libevm rlp and coreth customtypes at avalanchego 4f8c6316bed8.
package cblock

// Block is a coreth block: rlp([header, txs, uncles, version, extData])
// (libevm core/types extblock with coreth BlockBodyExtra fields Version, ExtData).
type Block struct {
	Header  Header
	Txs     []*Tx
	Uncles  []Header // coreth and SAE blocks carry none; decoded so that nothing is lost
	Version uint32
	ExtData []byte // atomic txs, kept as bytes

	// HeaderRLP is the header's encoding, a slice of the decoded input (nil for a built block).
	HeaderRLP []byte
}

// Hash is the block hash, keccak256 of the header RLP.
func (b *Block) Hash() [32]byte {
	if b.HeaderRLP != nil {
		return keccak(b.HeaderRLP)
	}
	return b.Header.Hash()
}

// Encode returns the block's RLP encoding, built from the fields (Tx.Enc and HeaderRLP are
// not used).
func (b *Block) Encode() []byte {
	dst := b.Header.AppendRLP(nil)
	s := len(dst)
	for _, t := range b.Txs {
		if t.Type == LegacyTxType {
			dst = t.appendEnc(dst)
		} else {
			dst = appendBytes(dst, t.appendEnc(nil))
		}
	}
	dst = wrapList(dst, s)
	s = len(dst)
	for i := range b.Uncles {
		dst = b.Uncles[i].AppendRLP(dst)
	}
	dst = wrapList(dst, s)
	dst = appendUint(dst, uint64(b.Version))
	dst = appendBytes(dst, b.ExtData)
	return wrapList(dst, 0)
}

// Decode decodes a coreth block encoding strictly (canonical RLP, exact element counts, no
// trailing bytes). Slices in the result alias b.
func Decode(b []byte) (*Block, error) {
	blk := new(Block)
	err := top(b, func(d *dec) {
		blk.HeaderRLP = d.raw()
		if d.err == nil {
			h, err := DecodeHeader(blk.HeaderRLP)
			d.fail(err)
			if h != nil {
				blk.Header = *h
			}
		}
		txs := d.list()
		for txs.more() {
			blk.Txs = append(blk.Txs, decodeBodyTx(txs))
		}
		d.absorb(txs)
		uncles := d.list()
		for uncles.more() {
			raw := uncles.raw()
			if uncles.err == nil {
				h, err := DecodeHeader(raw)
				uncles.fail(err)
				if h != nil {
					blk.Uncles = append(blk.Uncles, *h)
				}
			}
		}
		d.absorb(uncles)
		blk.Version = uint32(d.uint(4))
		blk.ExtData = d.bytes()
	})
	if err != nil {
		return nil, err
	}
	return blk, nil
}

// EmptyExtDataHash is keccak256(rlp("")), coreth customtypes.EmptyExtDataHash.
var EmptyExtDataHash = keccak([]byte{0x80})

// ExtDataHash is coreth CalcExtDataHash: EmptyExtDataHash for empty ext data, else
// keccak256(rlp(extData)).
func ExtDataHash(extData []byte) [32]byte {
	if len(extData) == 0 {
		return EmptyExtDataHash
	}
	return keccak(appendBytes(nil, extData))
}
