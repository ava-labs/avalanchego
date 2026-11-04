package tier0

import (
	"bytes"
	"errors"
	"fmt"
)

// Receipts are stored without the bloom (the storage form): the consensus encoding is
// [type byte] | rlp[status, cumulative gas, bloom, logs]; the stored record is
// [type byte] | rlp[status, cumulative gas, logs] with the other items' bytes unchanged. The
// bloom is a function of the logs (addresses and topics) and is rebuilt on read, so the
// consensus bytes come back exactly; Add rejects a receipt whose bloom is not its logs' bloom.

var errReceipt = errors.New("tier0: receipt is not a consensus receipt")

// rlpItem splits the first RLP item off b: its full bytes, its content, whether it is a list.
func rlpItem(b []byte) (item, content []byte, list bool, err error) {
	if len(b) == 0 {
		return nil, nil, false, errReceipt
	}
	p := b[0]
	var off, n int
	switch {
	case p < 0x80:
		return b[:1], b[:1], false, nil
	case p < 0xb8:
		off, n = 1, int(p-0x80)
	case p < 0xc0:
		off, n, err = longLen(b, int(p-0xb7))
	case p < 0xf8:
		off, n, list = 1, int(p-0xc0), true
	default:
		off, n, err = longLen(b, int(p-0xf7))
		list = true
	}
	if err != nil || n > len(b)-off {
		return nil, nil, false, errReceipt
	}
	return b[:off+n], b[off : off+n], list, nil
}

func longLen(b []byte, ll int) (off, n int, err error) {
	if ll > 4 || len(b) < 1+ll || b[1] == 0 {
		return 0, 0, errReceipt
	}
	for _, x := range b[1 : 1+ll] {
		n = n<<8 | int(x)
	}
	if n < 56 {
		return 0, 0, errReceipt
	}
	return 1 + ll, n, nil
}

// rlpListHead is the RLP header of a list with n content bytes.
func rlpListHead(n int) []byte {
	if n < 56 {
		return []byte{0xc0 + byte(n)}
	}
	var l []byte
	for x := n; x > 0; x >>= 8 {
		l = append([]byte{byte(x)}, l...)
	}
	return append([]byte{0xf7 + byte(len(l))}, l...)
}

// receiptParts splits a receipt (either form) into its type prefix and list items.
func receiptParts(r []byte) (typ []byte, items [][]byte, err error) {
	if len(r) > 0 && r[0] < 0x80 { // typed receipt: one type byte before the list
		typ, r = r[:1], r[1:]
	}
	whole, content, list, err := rlpItem(r)
	if err != nil || !list || len(whole) != len(r) {
		return nil, nil, errReceipt
	}
	for len(content) > 0 {
		it, _, _, err := rlpItem(content)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, it)
		content = content[len(it):]
	}
	return typ, items, nil
}

// logsBloom is the 2048-bit bloom of an RLP logs list (each log [address, [topics], data]).
func logsBloom(logs []byte) ([256]byte, error) {
	var bl [256]byte
	err := eachLogKey(logs, func(x []byte, _ bool) {
		h := keccak(x)
		for i := 0; i < 6; i += 2 {
			bit := (uint(h[i])<<8 | uint(h[i+1])) & 2047
			bl[255-bit/8] |= 1 << (bit % 8)
		}
	})
	return bl, err
}

// eachLogKey calls key with every log address (topic false, 20 B) and topic (topic true,
// 32 B) of an RLP logs list, in order.
func eachLogKey(logs []byte, key func(x []byte, topic bool)) error {
	_, content, list, err := rlpItem(logs)
	if err != nil || !list {
		return errReceipt
	}
	for len(content) > 0 {
		lg, lc, list, err := rlpItem(content)
		if err != nil || !list {
			return errReceipt
		}
		content = content[len(lg):]
		a, ac, alist, err := rlpItem(lc)
		if err != nil || alist || len(ac) != 20 {
			return errReceipt
		}
		key(ac, false)
		ts, tc, tlist, err := rlpItem(lc[len(a):])
		if err != nil || !tlist {
			return errReceipt
		}
		for len(tc) > 0 {
			t, tcc, tl, err := rlpItem(tc)
			if err != nil || tl || len(tcc) != 32 {
				return errReceipt
			}
			key(tcc, true)
			tc = tc[len(t):]
		}
		if _, _, dl, err := rlpItem(lc[len(a)+len(ts):]); err != nil || dl {
			return errReceipt
		}
	}
	return nil
}

// ReceiptLogs appends the log addresses and topics of a consensus receipt to addrs and topics
// (Block.LogAddrs and LogTopics, in log order).
func ReceiptLogs(rc []byte, addrs [][20]byte, topics [][32]byte) ([][20]byte, [][32]byte, error) {
	_, it, err := receiptParts(rc)
	if err != nil || len(it) != 4 {
		return addrs, topics, errReceipt
	}
	err = eachLogKey(it[3], func(x []byte, topic bool) {
		if topic {
			topics = append(topics, [32]byte(x))
		} else {
			addrs = append(addrs, [20]byte(x))
		}
	})
	return addrs, topics, err
}

var bloomHead = []byte{0xb9, 0x01, 0x00}

// storeReceipt drops the bloom from a consensus receipt after checking it equals the logs'.
func storeReceipt(r []byte) ([]byte, error) {
	typ, it, err := receiptParts(r)
	if err != nil || len(it) != 4 || len(it[2]) != 259 || !bytes.Equal(it[2][:3], bloomHead) {
		return nil, fmt.Errorf("%w (%d items)", errReceipt, len(it))
	}
	bl, err := logsBloom(it[3])
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(bl[:], it[2][3:]) {
		return nil, errors.New("tier0: receipt bloom is not the bloom of its logs")
	}
	n := len(it[0]) + len(it[1]) + len(it[3])
	out := append(append([]byte{}, typ...), rlpListHead(n)...)
	return append(append(append(out, it[0]...), it[1]...), it[3]...), nil
}

// consensusReceipt rebuilds the consensus encoding from a stored receipt.
func consensusReceipt(s []byte) ([]byte, error) {
	typ, it, err := receiptParts(s)
	if err != nil || len(it) != 3 {
		return nil, errCorrupt
	}
	bl, err := logsBloom(it[2])
	if err != nil {
		return nil, errCorrupt
	}
	n := len(it[0]) + len(it[1]) + 259 + len(it[2])
	out := append(append(make([]byte, 0, len(typ)+4+n), typ...), rlpListHead(n)...)
	out = append(append(append(out, it[0]...), it[1]...), bloomHead...)
	return append(append(out, bl[:]...), it[2]...), nil
}

// ReceiptCumGas is the cumulative gas field of a consensus receipt.
func ReceiptCumGas(rc []byte) (uint64, error) {
	_, it, err := receiptParts(rc)
	if err != nil || len(it) < 2 {
		return 0, errReceipt
	}
	_, c, list, err := rlpItem(it[1])
	if err != nil || list || len(c) > 8 || len(it[1]) == 1 && it[1][0] >= 0x80 && len(c) != 0 {
		return 0, errReceipt
	}
	if len(it[1]) == 1 && it[1][0] < 0x80 {
		return uint64(it[1][0]), nil
	}
	v := uint64(0)
	for _, x := range c {
		v = v<<8 | uint64(x)
	}
	return v, nil
}
