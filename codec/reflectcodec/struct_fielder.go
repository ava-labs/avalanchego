// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	UpgradeVersionIDFieldName = "UpgradeVersionID"
	UpgradeVersionTagName     = "upgradeVersion"
)

var _ StructFielder = (*structFielder)(nil)

type FieldDesc struct {
	Index          int
	MaxSliceLen    uint32
	UpgradeVersion uint16
}

type SerializedFields struct {
	Fields            []FieldDesc
	CheckUpgrade      bool
	MaxUpgradeVersion uint16
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
	GetSerializedFields(t reflect.Type) (*SerializedFields, error)
}

func NewStructFielder(tagNames []string, maxSliceLen uint32) StructFielder {
	return &structFielder{
		tags:                   tagNames,
		maxSliceLen:            maxSliceLen,
		serializedFieldIndices: make(map[reflect.Type]*SerializedFields),
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
	serializedFieldIndices map[reflect.Type]*SerializedFields
}

func (s *structFielder) GetSerializedFields(t reflect.Type) (*SerializedFields, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.serializedFieldIndices == nil {
		s.serializedFieldIndices = make(map[reflect.Type]*SerializedFields)
	}
	if serializedFields, ok := s.serializedFieldIndices[t]; ok { // use pre-computed result
		return serializedFields, nil
	}
	numFields := t.NumField()
	checkUpgrade := false
	startIndex := 0
	if numFields > 0 && t.Field(0).Type.Kind() == reflect.Uint64 &&
		t.Field(0).Name == UpgradeVersionIDFieldName {
		checkUpgrade = true
		startIndex = 1
	}
	serializedFields := &SerializedFields{Fields: make([]FieldDesc, 0, numFields), CheckUpgrade: checkUpgrade}
	maxUpgradeVersion := uint16(0)
	for i := startIndex; i < numFields; i++ { // Go through all fields of this struct
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

		upgradeVersionTag := field.Tag.Get(UpgradeVersionTagName)
		upgradeVersion := uint16(0)
		if upgradeVersionTag != "" {
			v, err := strconv.ParseUint(upgradeVersionTag, 10, 8)
			if err != nil {
				return nil, fmt.Errorf("can't parse %s (%s)", UpgradeVersionTagName, upgradeVersionTag)
			}
			upgradeVersion = uint16(v)
			maxUpgradeVersion = upgradeVersion
		}
		sliceLenField := field.Tag.Get(SliceLenTagName)
		maxSliceLen := s.maxSliceLen

		if newLen, err := strconv.ParseUint(sliceLenField, 10, 31); err == nil {
			maxSliceLen = uint32(newLen)
		}
		serializedFields.Fields = append(serializedFields.Fields, FieldDesc{
			Index:          i,
			MaxSliceLen:    maxSliceLen,
			UpgradeVersion: upgradeVersion,
		})
	}
	serializedFields.MaxUpgradeVersion = maxUpgradeVersion
	s.serializedFieldIndices[t] = serializedFields // cache result
	return serializedFields, nil
}
