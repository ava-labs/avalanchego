// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
)

const (
	firstCustomTypeID = 8192
)

var (
	_ CaminoCodec          = (*caminoLinearCodec)(nil)
	_ codec.Codec          = (*caminoLinearCodec)(nil)
	_ codec.CaminoRegistry = (*caminoLinearCodec)(nil)
	_ codec.GeneralCodec   = (*caminoLinearCodec)(nil)
)

// Codec marshals and unmarshals
type CaminoCodec interface {
	codec.CaminoRegistry
	codec.Codec
	SkipRegistrations(int)
	SkipCustomRegistrations(int)
}

type caminoLinearCodec struct {
	linearCodec
	nextCustomTypeID uint32
}

func NewCamino(tagNames []string, maxSliceLen uint32) CaminoCodec {
	hCodec := &caminoLinearCodec{
		linearCodec: linearCodec{
			nextTypeID:   0,
			typeIDToType: map[uint32]reflect.Type{},
			typeToTypeID: map[reflect.Type]uint32{},
		},
		nextCustomTypeID: firstCustomTypeID,
	}
	hCodec.Codec = reflectcodec.New(hCodec, tagNames, maxSliceLen)
	return hCodec
}

// NewDefault is a convenience constructor; it returns a new codec with reasonable default values
func NewCaminoDefault() CaminoCodec {
	return NewCamino([]string{reflectcodec.DefaultTagName}, defaultMaxSliceLength)
}

// NewCustomMaxLength is a convenience constructor; it returns a new codec with custom max length and default tags
func NewCaminoCustomMaxLength(maxSliceLen uint32) CaminoCodec {
	return NewCamino([]string{reflectcodec.DefaultTagName}, maxSliceLen)
}

// RegisterCustomType is used to register custom types that may be
// unmarshaled into an interface
// [val] is a value of the type being registered
func (c *caminoLinearCodec) RegisterCustomType(val interface{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	valType := reflect.TypeOf(val)
	if _, exists := c.typeToTypeID[valType]; exists {
		return fmt.Errorf("type %v has already been registered", valType)
	}

	c.typeIDToType[c.nextCustomTypeID] = valType
	c.typeToTypeID[valType] = c.nextCustomTypeID
	c.nextCustomTypeID++
	return nil
}

// Skip some number of type IDs
func (c *caminoLinearCodec) SkipCustomRegistrations(num int) {
	c.lock.Lock()
	c.nextCustomTypeID += uint32(num)
	c.lock.Unlock()
}
