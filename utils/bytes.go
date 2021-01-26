// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// CopyBytes returns a copy of the provided byte slice. If nil is provided, nil
// will be returned.
func CopyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}

	cb := make([]byte, len(b))
	copy(cb, b)
	return cb
}

// MarshalBytes returns []byte(v) if v is a string
// or marshals v as JSON bytes.
func MarshalBytes(v interface{}) ([]byte, error) {
	switch value := v.(type) {
	case string:
		return []byte(value), nil
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("could not marshal JSON: %w", err)
		}
		return b, nil
	}
}

// SetByteSlices calls MarshalBytes on
// the value of all keys found in source that are
// also a field in v, setting the value of the field
// to the result.
//
// Note: Field names are lowercased before looking
// up corresponding keys in source.
//
// Note: v MUST be a pointer for this function
// to work correctly.
func SetByteSlices(source map[string]interface{}, v interface{}) error {
	s := reflect.ValueOf(v).Elem()
	if destKind := s.Kind(); destKind != reflect.Struct {
		return fmt.Errorf("cannot set byte slices on %s", destKind)
	}

	for i := 0; i < s.NumField(); i++ {
		field := s.Field(i)
		fieldType := s.Type().Field(i)
		if fieldType.Type.Kind() != reflect.Slice {
			continue
		}

		if fieldType.Type.Elem().Kind() != reflect.Uint8 {
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

		b, err := MarshalBytes(v)
		if err != nil {
			return fmt.Errorf("could not parse %+v: %w", v, err)
		}

		field.Set(reflect.ValueOf(b))
	}

	return nil
}
