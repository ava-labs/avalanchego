// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// DecodeStringOrJSON returns v if v is a string
// or marshals v as JSON.
func DecodeStringOrJSON(v interface{}) (string, error) {
	switch value := v.(type) {
	case string:
		return value, nil
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("could not marshal JSON: %w", err)
		}
		return string(b), nil
	}
}

// PopulateStringFields calls decodeStringOrJSON on
// the value of all keys found in source that are
// also a field in v.
//
// Note: v MUST be a pointer for this function
// to work correctly.
func PopulateStringFields(source map[string]interface{}, v interface{}) error {
	s := reflect.ValueOf(v).Elem()
	for i := 0; i < s.NumField(); i++ {
		field := s.Field(i)
		fieldType := s.Type().Field(i)
		if fieldType.Type.Kind() != reflect.String {
			continue
		}

		if !field.CanSet() {
			continue
		}

		name := strings.ToLower(fieldType.Name)
		v, ok := source[name]
		if !ok {
			continue
		}

		parsedValue, err := DecodeStringOrJSON(v)
		if err != nil {
			return fmt.Errorf("could not parse %+v: %w", v, err)
		}

		field.Set(reflect.ValueOf(parsedValue))
	}

	return nil
}
