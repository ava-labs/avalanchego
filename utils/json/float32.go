// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import (
	"strconv"
)

type Float32 float32

func (f Float32) MarshalJSON() ([]byte, error) {
	return []byte("\"" + strconv.FormatFloat(float64(f), byte('f'), 4, 32) + "\""), nil
}

func (f *Float32) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == Null {
		return nil
	}
	if len(str) >= 2 {
		if lastIndex := len(str) - 1; str[0] == '"' && str[lastIndex] == '"' {
			str = str[1:lastIndex]
		}
	}
	val, err := strconv.ParseFloat(str, 32)
	*f = Float32(val)
	return err
}
