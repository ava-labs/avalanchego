// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import "strconv"

type Uint8 uint8

func (u Uint8) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(uint64(u), 10) + `"`), nil
}

func (u *Uint8) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == Null {
		return nil
	}
	if len(str) >= 2 {
		if lastIndex := len(str) - 1; str[0] == '"' && str[lastIndex] == '"' {
			str = str[1:lastIndex]
		}
	}
	val, err := strconv.ParseUint(str, 10, 8)
	*u = Uint8(val)
	return err
}
