// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/ava-labs/libevm/rlp"
)

// SpliceBlockRLP converts a block's stored header and body encodings, as
// written by [rawdb.WriteBlock], into the encoding of the whole block, i.e.
// that returned by [Block.Bytes], by wrapping the concatenation of the header
// encoding and the body's list payload in a new RLP list. No decoding is
// performed.
//
// This is only correct while the process-wide registered
// [types.BlockBodyHooks] encode a block as its header followed by its body's
// fields. Both the default and the C-Chain hooks do, and tests in this
// package and in the C-Chain package fail loudly if either stops.
//
// [rawdb.WriteBlock]: https://pkg.go.dev/github.com/ava-labs/libevm/core/rawdb#WriteBlock
func SpliceBlockRLP(header, body rlp.RawValue) ([]byte, error) {
	if _, rest, err := rlp.SplitList(header); err != nil {
		return nil, fmt.Errorf("splitting header: %w", err)
	} else if len(rest) > 0 {
		return nil, fmt.Errorf("splitting header: %w", errTrailingBytes)
	}
	payload, rest, err := rlp.SplitList(body)
	if err != nil {
		return nil, fmt.Errorf("splitting body: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("splitting body: %w", errTrailingBytes)
	}

	size := len(header) + len(payload)
	out := appendListHeader(make([]byte, 0, maxListHeaderLen+size), size)
	out = append(out, header...)
	return append(out, payload...), nil
}

var errTrailingBytes = errors.New("trailing bytes")

// maxListHeaderLen is the maximum number of bytes written by
// [appendListHeader], being the tag byte plus a uint64 payload size.
const maxListHeaderLen = 9

// appendListHeader appends the RLP header of a list with the given payload
// size.
func appendListHeader(dst []byte, size int) []byte {
	if size < 56 {
		return append(dst, 0xC0+byte(size))
	}
	n := (bits.Len64(uint64(size)) + 7) / 8 //#nosec G115 -- non-negative slice length
	dst = append(dst, 0xF7+byte(n))
	for i := n - 1; i >= 0; i-- {
		dst = append(dst, byte(size>>(8*i)))
	}
	return dst
}
