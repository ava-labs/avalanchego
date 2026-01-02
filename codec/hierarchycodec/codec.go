// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hierarchycodec

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
	_ Codec              = (*hierarchyCodec)(nil)
	_ codec.Codec        = (*hierarchyCodec)(nil)
	_ codec.Registry     = (*hierarchyCodec)(nil)
	_ codec.GeneralCodec = (*hierarchyCodec)(nil)
)

// Codec marshals and unmarshals
type Codec interface {
	codec.Registry
	codec.Codec
	SkipRegistrations(int)
	NextGroup()
}

type typeID struct {
	groupID uint16
	typeID  uint16
}

// Codec handles marshaling and unmarshaling of structs
type hierarchyCodec struct {
	codec.Codec

	lock            sync.RWMutex
	currentGroupID  uint16
	nextTypeID      uint16
	registeredTypes *bimap.BiMap[typeID, reflect.Type]
}

// New returns a new, concurrency-safe codec
func New(tagNames []string) Codec {
	hCodec := &hierarchyCodec{
		currentGroupID:  0,
		nextTypeID:      0,
		registeredTypes: bimap.New[typeID, reflect.Type](),
	}
	hCodec.Codec = reflectcodec.New(hCodec, tagNames)
	return hCodec
}

// NewDefault returns a new codec with reasonable default values
func NewDefault() Codec {
	return New([]string{reflectcodec.DefaultTagName})
}

// SkipRegistrations some number of type IDs
func (c *hierarchyCodec) SkipRegistrations(num int) {
	c.lock.Lock()
	c.nextTypeID += uint16(num)
	c.lock.Unlock()
}

// NextGroup moves to the next group registry
func (c *hierarchyCodec) NextGroup() {
	c.lock.Lock()
	c.currentGroupID++
	c.nextTypeID = 0
	c.lock.Unlock()
}

// RegisterType is used to register types that may be unmarshaled into an interface
// [val] is a value of the type being registered
func (c *hierarchyCodec) RegisterType(val interface{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	valType := reflect.TypeOf(val)
	if c.registeredTypes.HasValue(valType) {
		return fmt.Errorf("%w: %v", codec.ErrDuplicateType, valType)
	}

	valTypeID := typeID{
		groupID: c.currentGroupID,
		typeID:  c.nextTypeID,
	}
	c.nextTypeID++

	c.registeredTypes.Put(valTypeID, valType)
	return nil
}

func (*hierarchyCodec) PrefixSize(reflect.Type) int {
	// see PackPrefix implementation
	return wrappers.ShortLen + wrappers.ShortLen
}

func (c *hierarchyCodec) PackPrefix(p *wrappers.Packer, valueType reflect.Type) error {
	c.lock.RLock()
	defer c.lock.RUnlock()

	typeID, ok := c.registeredTypes.GetKey(valueType) // Get the type ID of the value being marshaled
	if !ok {
		return fmt.Errorf("can't marshal unregistered type %q", valueType)
	}
	// Pack type ID so we know what to unmarshal this into
	p.PackShort(typeID.groupID)
	p.PackShort(typeID.typeID)
	return p.Err
}

func (c *hierarchyCodec) UnpackPrefix(p *wrappers.Packer, valueType reflect.Type) (reflect.Value, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	groupID := p.UnpackShort()     // Get the group ID
	typeIDShort := p.UnpackShort() // Get the type ID
	if p.Err != nil {
		return reflect.Value{}, fmt.Errorf("couldn't unmarshal interface: %w", p.Err)
	}
	t := typeID{
		groupID: groupID,
		typeID:  typeIDShort,
	}
	// Get a type that implements the interface
	implementingType, ok := c.registeredTypes.GetValue(t)
	if !ok {
		return reflect.Value{}, fmt.Errorf("couldn't unmarshal interface: unknown type ID %+v", t)
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
