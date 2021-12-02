// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import (
	"strconv"
)

type Uint16 uint16

func (u Uint16) MarshalJSON() ([]byte, error) {
	return []byte("\"" + strconv.FormatUint(uint64(u), 10) + "\""), nil
}

func (u *Uint16) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == Null {
		return nil
	}
	if len(str) >= 2 {
		if lastIndex := len(str) - 1; str[0] == '"' && str[lastIndex] == '"' {
			str = str[1:lastIndex]
		}
	}
	val, err := strconv.ParseUint(str, 10, 16)
	*u = Uint16(val)
	return err
}
