// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reflectcodec

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

const (
	// SliceLenTagName that specifies the length of a slice.
	SliceLenTagName = "len"

	// TagValue is the value the tag must have to be serialized.
	TagValue = "true"
)

var _ StructFielder = &structFielder{}

type FieldDesc struct {
	Index       int
	MaxSliceLen uint32
}

// StructFielder handles discovery of serializable fields in a struct.
type StructFielder interface {
	// Returns the fields that have been marked as serializable in [t], which is
	// a struct type. Additionally, returns the custom maximum length slice that
	// may be serialized into the field, if any.
	// Returns an error if a field has tag "[tagName]: [TagValue]" but the field
	// is un-exported.
	// GetSerializedField(Foo) --> [1,5,8] means Foo.Field(1), Foo.Field(5),
	// Foo.Field(8) are to be serialized/deserialized.
	GetSerializedFields(t reflect.Type) ([]FieldDesc, error)
}

func NewStructFielder(tagNames []string, maxSliceLen uint32) StructFielder {
	return &structFielder{
		tags:                   tagNames,
		maxSliceLen:            maxSliceLen,
		serializedFieldIndices: make(map[reflect.Type][]FieldDesc),
	}
}

type structFielder struct {
	lock sync.Mutex

	// multiple tags per field can be specified. A field is serialized/deserialized
	// if it has at least one of the specified tags.
	tags []string

	maxSliceLen uint32

	// Key: a struct type
	// Value: Slice where each element is index in the struct type of a field
	// that is serialized/deserialized e.g. Foo --> [1,5,8] means Foo.Field(1),
	// etc. are to be serialized/deserialized. We assume this cache is pretty
	// small (a few hundred keys at most) and doesn't take up much memory.
	serializedFieldIndices map[reflect.Type][]FieldDesc
}

func (s *structFielder) GetSerializedFields(t reflect.Type) ([]FieldDesc, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.serializedFieldIndices == nil {
		s.serializedFieldIndices = make(map[reflect.Type][]FieldDesc)
	}
	if serializedFields, ok := s.serializedFieldIndices[t]; ok { // use pre-computed result
		return serializedFields, nil
	}
	numFields := t.NumField()
	serializedFields := make([]FieldDesc, 0, numFields)
	for i := 0; i < numFields; i++ { // Go through all fields of this struct
		field := t.Field(i)

		// Multiple tags per fields can be specified.
		// Serialize/Deserialize field if it has
		// any tag with the right value
		captureField := false
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
			return nil, fmt.Errorf("can't marshal un-exported field %s", field.Name)
		}
		sliceLenField := field.Tag.Get(SliceLenTagName)
		maxSliceLen := s.maxSliceLen

		if newLen, err := strconv.ParseUint(sliceLenField, 10, 31); err == nil {
			maxSliceLen = uint32(newLen)
		}
		serializedFields = append(serializedFields, FieldDesc{
			Index:       i,
			MaxSliceLen: maxSliceLen,
		})
	}
	s.serializedFieldIndices[t] = serializedFields // cache result
	return serializedFields, nil
}
