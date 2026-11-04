package cblock

import (
	"errors"
	"fmt"
)

// MainnetApricotPhase1Time is the mainnet Apricot Phase 1 activation (avalanchego
// upgrade/upgrade.go: 2021-03-31 14:00 UTC) as unix seconds.
const MainnetApricotPhase1Time = 1617199200

// EmptyRootHash is the root of an empty Merkle Patricia trie, keccak256(rlp("")).
var EmptyRootHash = keccak([]byte{0x80})

var (
	errTxRoot     = errors.New("cblock: transactions root does not match the header")
	errUncleHash  = errors.New("cblock: uncle hash does not match the header")
	errVersion    = errors.New("cblock: block version is not 0")
	errExtDataCom = errors.New("cblock: ext data hash does not match the header")
)

// CheckBody checks that the body is the one the header commits to, as SAE does when it
// parses a C-Chain block (vms/saevm/blocks/parse.go parseEthBlock: transactions root and
// uncle hash; cchain/hooks.go VerifyBlockSyntax: version 0 and ExtDataHash). The ext data
// rule is checked for mainnet blocks from Apricot Phase 1 on (Time >= MainnetApricotPhase1Time,
// height > 0): the header's ExtDataHash must equal ExtDataHash(ExtData). Earlier blocks commit
// their ext data through avalanchego's hardcoded extdata-mainnet.json table (5 MB), which
// cblock does not carry; for them only the zero header ExtDataHash is checked.
func (b *Block) CheckBody() error {
	if TxRoot(b.Txs) != b.Header.TxHash {
		return errTxRoot
	}
	dst := []byte{}
	for i := range b.Uncles {
		dst = b.Uncles[i].AppendRLP(dst)
	}
	if keccak(wrapList(dst, 0)) != b.Header.UncleHash {
		return errUncleHash
	}
	if b.Version != 0 {
		return fmt.Errorf("%w: %d", errVersion, b.Version)
	}
	want := ExtDataHash(b.ExtData)
	if b.Header.Number == 0 || b.Header.Time < MainnetApricotPhase1Time {
		want = [32]byte{}
	}
	if b.Header.ExtDataHash != want {
		return errExtDataCom
	}
	return nil
}

// TxRoot is the transactions root (libevm types.DeriveSha with a stack trie): the root of
// the Merkle Patricia trie mapping rlp(index) to each transaction's canonical encoding.
func TxRoot(txs []*Tx) [32]byte {
	if len(txs) == 0 {
		return EmptyRootHash
	}
	keys := make([][]byte, len(txs))
	vals := make([][]byte, len(txs))
	for i, t := range txs {
		keys[i] = nibbles(appendUint(nil, uint64(i)))
		vals[i] = t.appendEnc(nil)
	}
	// Sort by key: rlp(i) for i in 1..127 is the byte i, rlp(0) is 0x80, and from 128 on the
	// keys start with 0x81.. and sort by value, so the order is 1..127, 0, 128...
	order := make([]int, 0, len(txs))
	for i := 1; i < len(txs) && i < 128; i++ {
		order = append(order, i)
	}
	order = append(order, 0)
	for i := 128; i < len(txs); i++ {
		order = append(order, i)
	}
	ks := make([][]byte, len(order))
	vs := make([][]byte, len(order))
	for j, i := range order {
		ks[j], vs[j] = keys[i], vals[i]
	}
	return keccak(trieNode(ks, vs, 0))
}

func nibbles(b []byte) []byte {
	n := make([]byte, 2*len(b))
	for i, c := range b {
		n[2*i], n[2*i+1] = c>>4, c&15
	}
	return n
}

// hexPrefix is the compact encoding of a nibble path (Ethereum yellow paper appendix C).
func hexPrefix(path []byte, leaf bool) []byte {
	flag := byte(0)
	if leaf {
		flag = 2
	}
	var out []byte
	if len(path)%2 == 1 {
		out = append(out, (flag+1)<<4|path[0])
		path = path[1:]
	} else {
		out = append(out, flag<<4)
	}
	for i := 0; i < len(path); i += 2 {
		out = append(out, path[i]<<4|path[i+1])
	}
	return out
}

// trieNode returns the RLP encoding of the node holding the sorted, prefix-free keys (all
// sharing their first depth nibbles) with their values.
func trieNode(keys, vals [][]byte, depth int) []byte {
	if len(keys) == 1 {
		dst := appendBytes(nil, hexPrefix(keys[0][depth:], true))
		dst = appendBytes(dst, vals[0])
		return wrapList(dst, 0)
	}
	// Common prefix beyond depth: an extension node.
	first, last := keys[0], keys[len(keys)-1]
	n := 0
	for depth+n < len(first) && depth+n < len(last) && first[depth+n] == last[depth+n] {
		n++
	}
	if n > 0 {
		dst := appendBytes(nil, hexPrefix(first[depth:depth+n], false))
		dst = appendRef(dst, trieNode(keys, vals, depth+n))
		return wrapList(dst, 0)
	}
	// Branch node: 16 children by the nibble at depth, no value (keys are prefix-free).
	var dst []byte
	i := 0
	for nib := byte(0); nib < 16; nib++ {
		j := i
		for j < len(keys) && keys[j][depth] == nib {
			j++
		}
		if j == i {
			dst = append(dst, 0x80)
		} else {
			dst = appendRef(dst, trieNode(keys[i:j], vals[i:j], depth+1))
		}
		i = j
	}
	dst = append(dst, 0x80)
	return wrapList(dst, 0)
}

// appendRef appends a child reference: the node itself if its encoding is shorter than 32
// bytes, else its keccak256 hash.
func appendRef(dst, node []byte) []byte {
	if len(node) < 32 {
		return append(dst, node...)
	}
	h := keccak(node)
	return appendBytes(dst, h[:])
}
