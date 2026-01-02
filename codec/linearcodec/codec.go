// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/utils/bimap"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ Codec              = (*linearCodec)(nil)
	_ codec.Codec        = (*linearCodec)(nil)
	_ codec.Registry     = (*linearCodec)(nil)
	_ codec.GeneralCodec = (*linearCodec)(nil)
)

// Codec marshals and unmarshals
type Codec interface {
	codec.Registry
	codec.Codec
	SkipRegistrations(int)
}

// Codec handles marshaling and unmarshaling of structs
type linearCodec struct {
	codec.Codec

	lock            sync.RWMutex
	nextTypeID      uint32
	registeredTypes *bimap.BiMap[uint32, reflect.Type]
}

// New returns a new, concurrency-safe codec; it allow to specify tagNames.
func New(tagNames []string) Codec {
	hCodec := &linearCodec{
		nextTypeID:      0,
		registeredTypes: bimap.New[uint32, reflect.Type](),
	}
	hCodec.Codec = reflectcodec.New(hCodec, tagNames)
	return hCodec
}

// NewDefault is a convenience constructor; it returns a new codec with default
// tagNames.
func NewDefault() Codec {
	return New([]string{reflectcodec.DefaultTagName})
}

// Skip some number of type IDs
func (c *linearCodec) SkipRegistrations(num int) {
	c.lock.Lock()
	c.nextTypeID += uint32(num)
	c.lock.Unlock()
}

// RegisterType is used to register types that may be unmarshaled into an interface
// [val] is a value of the type being registered
func (c *linearCodec) RegisterType(val interface{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	valType := reflect.TypeOf(val)
	if c.registeredTypes.HasValue(valType) {
		return fmt.Errorf("%w: %v", codec.ErrDuplicateType, valType)
	}

	c.registeredTypes.Put(c.nextTypeID, valType)
	c.nextTypeID++
	return nil
}

func (*linearCodec) PrefixSize(reflect.Type) int {
	// see PackPrefix implementation
	return wrappers.IntLen
}

func (c *linearCodec) PackPrefix(p *wrappers.Packer, valueType reflect.Type) error {
	c.lock.RLock()
	defer c.lock.RUnlock()

	typeID, ok := c.registeredTypes.GetKey(valueType) // Get the type ID of the value being marshaled
	if !ok {
		return fmt.Errorf("can't marshal unregistered type %q", valueType)
	}
	p.PackInt(typeID) // Pack type ID so we know what to unmarshal this into
	return p.Err
}

func (c *linearCodec) UnpackPrefix(p *wrappers.Packer, valueType reflect.Type) (reflect.Value, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	typeID := p.UnpackInt() // Get the type ID
	if p.Err != nil {
		return reflect.Value{}, fmt.Errorf("couldn't unmarshal interface: %w", p.Err)
	}
	// Get a type that implements the interface
	implementingType, ok := c.registeredTypes.GetValue(typeID)
	if !ok {
		return reflect.Value{}, fmt.Errorf("couldn't unmarshal interface: unknown type ID %d", typeID)
	}
	// Ensure type actually does implement the interface
	if !implementingType.Implements(valueType) {
		return reflect.Value{}, fmt.Errorf("couldn't unmarshal interface: %s %w %s",
			implementingType,
			codec.ErrDoesNotImplementInterface,
			valueType,
		)
	}
	return reflect.New(implementingType).Elem(), nil // instance of the proper type
}
