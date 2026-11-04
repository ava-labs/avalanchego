package asmevm

import "sort"

// Receipt is the consensus part of a transaction receipt.
type Receipt struct {
	Type    uint8 // 0 legacy, else the EIP-2718 type byte prefixes the RLP
	Success bool
	CumGas  uint64 // cumulative gas used in its own block
	Logs    []Log
}

// Bloom is the 2048-bit log bloom of logs.
func Bloom(logs []Log) (b [256]byte) {
	add := func(x []byte) {
		h := Keccak256(x)
		for i := 0; i < 6; i += 2 {
			bit := (uint(h[i])<<8 | uint(h[i+1])) & 2047
			b[255-bit/8] |= 1 << (bit % 8)
		}
	}
	for _, l := range logs {
		add(l.Address[:])
		for _, t := range l.Topics {
			add(t[:])
		}
	}
	return b
}

// EncodeReceipt is the consensus encoding: [type ||] rlp([status, cumGas, bloom, logs]).
func EncodeReceipt(r *Receipt) []byte {
	status := []byte{0x80}
	if r.Success {
		status = []byte{0x01}
	}
	bloom := Bloom(r.Logs)
	body := append(status, rlpUint(r.CumGas)...)
	body = append(body, rlpString(bloom[:])...)
	body = append(body, rlpLogs(r.Logs)...)
	enc := rlpList(body)
	if r.Type != 0 {
		enc = append([]byte{r.Type}, enc...)
	}
	return enc
}

// ReceiptsRoot is DeriveSha: the Merkle-Patricia trie root of
// rlp(index) -> EncodeReceipt(receipt).
func ReceiptsRoot(rs []Receipt) Hash {
	type kv struct{ k, v []byte }
	items := make([]kv, len(rs))
	for i := range rs {
		items[i] = kv{nibbles(rlpUint(uint64(i))), EncodeReceipt(&rs[i])}
	}
	sort.Slice(items, func(i, j int) bool { return string(items[i].k) < string(items[j].k) })
	keys, vals := make([][]byte, len(items)), make([][]byte, len(items))
	for i, it := range items {
		keys[i], vals[i] = it.k, it.v
	}
	if len(keys) == 0 {
		return Hash(Keccak256([]byte{0x80}))
	}
	return Hash(Keccak256(trieNode(keys, vals, 0)))
}

func nibbles(b []byte) []byte {
	n := make([]byte, 0, 2*len(b))
	for _, x := range b {
		n = append(n, x>>4, x&15)
	}
	return n
}

// hexPrefix is the compact encoding of a nibble path.
func hexPrefix(n []byte, leaf bool) []byte {
	f := byte(0)
	if leaf {
		f = 2
	}
	var out []byte
	if len(n)%2 == 1 {
		out = append(out, (f+1)<<4|n[0])
		n = n[1:]
	} else {
		out = append(out, f<<4)
	}
	for i := 0; i < len(n); i += 2 {
		out = append(out, n[i]<<4|n[i+1])
	}
	return out
}

// trieRef embeds a node shorter than 32 bytes, else references its hash.
func trieRef(enc []byte) []byte {
	if len(enc) < 32 {
		return enc
	}
	h := Keccak256(enc)
	return rlpString(h[:])
}

// trieNode encodes the node for sorted nibble keys sharing the first d nibbles.
func trieNode(keys, vals [][]byte, d int) []byte {
	if len(keys) == 1 {
		return rlpList(append(rlpString(hexPrefix(keys[0][d:], true)), rlpString(vals[0])...))
	}
	first, last := keys[0], keys[len(keys)-1]
	cp := 0
	for d+cp < len(first) && d+cp < len(last) && first[d+cp] == last[d+cp] {
		cp++
	}
	if cp > 0 {
		return rlpList(append(rlpString(hexPrefix(first[d:d+cp], false)), trieRef(trieNode(keys, vals, d+cp))...))
	}
	var body, value []byte
	i := 0
	if len(keys[0]) == d { // a key ending here is the branch value
		value = vals[0]
		i = 1
	}
	for nib := byte(0); nib < 16; nib++ {
		j := i
		for j < len(keys) && keys[j][d] == nib {
			j++
		}
		if j == i {
			body = append(body, 0x80)
		} else {
			body = append(body, trieRef(trieNode(keys[i:j], vals[i:j], d+1))...)
		}
		i = j
	}
	body = append(body, rlpString(value)...)
	return rlpList(body)
}

// rlpLogs is rlp(logs), each log rlp([address, [topics], data]).
func rlpLogs(logs []Log) []byte {
	var all []byte
	for _, l := range logs {
		var topics []byte
		for _, tp := range l.Topics {
			topics = append(topics, rlpString(tp[:])...)
		}
		item := append(rlpString(l.Address[:]), rlpList(topics)...)
		item = append(item, rlpString(l.Data)...)
		all = append(all, rlpList(item)...)
	}
	return rlpList(all)
}

func rlpHeader(base byte, n int) []byte {
	if n <= 55 {
		return []byte{base + byte(n)}
	}
	var lb []byte
	for v := n; v > 0; v >>= 8 {
		lb = append([]byte{byte(v)}, lb...)
	}
	return append([]byte{base + 55 + byte(len(lb))}, lb...)
}

func rlpString(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return []byte{b[0]}
	}
	return append(rlpHeader(0x80, len(b)), b...)
}

func rlpList(payload []byte) []byte { return append(rlpHeader(0xc0, len(payload)), payload...) }

// ReceiptsRootOf is DeriveSha over consensus-encoded receipts.
func ReceiptsRootOf(enc [][]byte) Hash {
	type kv struct{ k, v []byte }
	if len(enc) == 0 {
		return Hash(Keccak256([]byte{0x80}))
	}
	items := make([]kv, len(enc))
	for i := range enc {
		items[i] = kv{nibbles(rlpUint(uint64(i))), enc[i]}
	}
	sortKV := func(a, b kv) bool { return string(a.k) < string(b.k) }
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && sortKV(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	keys, vals := make([][]byte, len(items)), make([][]byte, len(items))
	for i, it := range items {
		keys[i], vals[i] = it.k, it.v
	}
	return Hash(Keccak256(trieNode(keys, vals, 0)))
}
