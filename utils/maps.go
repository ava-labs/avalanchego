// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"
	"fmt"
)

// MarshalMapSlice returns []map[string]interface{} if v is a
// string representing a JSON array or is a []interface{}.
func MarshalMapSlice(v interface{}) ([]map[string]interface{}, error) {
	var s []map[string]interface{}

	switch value := v.(type) {
	case []interface{}: // config file
		for i, o := range value {
			m, ok := o.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("could not cast object at index %d (`%+v`) to map[string]interface{}", i, o)
			}
			s = append(s, m)
		}
	case string: // command line
		if len(value) == 0 {
			return s, nil
		}

		var m []map[string]interface{}
		if err := json.Unmarshal([]byte(value), &m); err != nil {
			return nil, fmt.Errorf("could not unmarshal `%s` to []map[string]interface{}: %w", value, err)
		}
		s = append(s, m...)
	default:
		return nil, fmt.Errorf("could not marshal []map[string]interface{} for type `%T`", value)
	}

	return s, nil
}
