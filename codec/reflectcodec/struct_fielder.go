// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reflectcodec

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
)

// TagValue is the value the tag must have to be serialized.
const TagValue = "true"

var _ StructFielder = (*structFielder)(nil)

// StructFielder handles discovery of serializable fields in a struct.
type StructFielder interface {
	// Returns the fields that have been marked as serializable in [t], which is
	// a struct type.
	// Returns an error if a field has tag "[tagName]: [TagValue]" but the field
	// is un-exported.
	// GetSerializedField(Foo) --> [1,5,8] means Foo.Field(1), Foo.Field(5),
	// Foo.Field(8) are to be serialized/deserialized.
	GetSerializedFields(t reflect.Type) ([]int, error)
}

func NewStructFielder(tagNames []string) StructFielder {
	return &structFielder{
		tags:                   tagNames,
		serializedFieldIndices: make(map[reflect.Type][]int),
	}
}

type structFielder struct {
	lock sync.RWMutex

	// multiple tags per field can be specified. A field is serialized/deserialized
	// if it has at least one of the specified tags.
	tags []string

	// Key: a struct type
	// Value: Slice where each element is index in the struct type of a field
	// that is serialized/deserialized e.g. Foo --> [1,5,8] means Foo.Field(1),
	// etc. are to be serialized/deserialized. We assume this cache is pretty
	// small (a few hundred keys at most) and doesn't take up much memory.
	serializedFieldIndices map[reflect.Type][]int
}

func (s *structFielder) GetSerializedFields(t reflect.Type) ([]int, error) {
	if serializedFields, ok := s.getCachedSerializedFields(t); ok { // use pre-computed result
		return serializedFields, nil
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	numFields := t.NumField()
	serializedFields := make([]int, 0, numFields)
	for i := 0; i < numFields; i++ { // Go through all fields of this struct
		field := t.Field(i)

		// Multiple tags per fields can be specified.
		// Serialize/Deserialize field if it has
		// any tag with the right value
		var captureField bool
		for _, tag := range s.tags {
			if field.Tag.Get(tag) == TagValue {
				captureField = true
				break
			}
		}
		if !captureField {
			continue
		}
		if !field.IsExported() { // Can only marshal exported fields
			return nil, fmt.Errorf("can not marshal %w: %s",
				codec.ErrUnexportedField,
				field.Name,
			)
		}
		serializedFields = append(serializedFields, i)
	}
	s.serializedFieldIndices[t] = serializedFields // cache result
	return serializedFields, nil
}

func (s *structFielder) getCachedSerializedFields(t reflect.Type) ([]int, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	cachedFields, ok := s.serializedFieldIndices[t]
	return cachedFields, ok
}
