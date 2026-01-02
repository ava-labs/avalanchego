// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import "strconv"

type Float64 float64

func (f Float64) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatFloat(float64(f), byte('f'), 4, 64) + `"`), nil
}

func (f *Float64) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == Null {
		return nil
	}
	if len(str) >= 2 {
		if lastIndex := len(str) - 1; str[0] == '"' && str[lastIndex] == '"' {
			str = str[1:lastIndex]
		}
	}
	val, err := strconv.ParseFloat(str, 64)
	*f = Float64(val)
	return err
}
