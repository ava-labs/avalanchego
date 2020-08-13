package json

import (
	"math"
	"strconv"
)

// Float32 ...
type Float32 float32

// MarshalJSON ...
func (f Float32) MarshalJSON() ([]byte, error) {
	//return []byte("\"" + strconv.FormatUint(uint64(u), 10) + "\""), nil
	return []byte("\"" + strconv.FormatFloat(float64(f), byte('f'), 4, 32) + "\""), nil
}

// UnmarshalJSON ...
func (f *Float32) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}
	if len(str) >= 2 {
		if lastIndex := len(str) - 1; str[0] == '"' && str[lastIndex] == '"' {
			str = str[1:lastIndex]
		}
	}
	val, err := strconv.ParseFloat(str, 32)
	if val > math.MaxUint32 {
		return errTooLarge32
	}
	*f = Float32(val)
	return err
}
