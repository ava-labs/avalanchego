// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import "encoding/binary"

// FromInt converts an int to an ID
//
// Examples:
//
//	FromInt(0).Hex() == "0000000000000000000000000000000000000000000000000000000000000000"
//	FromInt(1).Hex() == "0000000000000000000000000000000000000000000000000000000000000001"
//	FromInt(math.MaxUint64).Hex() == "000000000000000000000000000000000000000000000000ffffffffffffffff"
func FromInt(idx uint64) ID {
	bytes := ID{}
	binary.BigEndian.PutUint64(bytes[24:], idx)

	return bytes
}
