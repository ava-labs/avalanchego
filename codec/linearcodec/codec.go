// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"unicode"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// default max length of a slice being marshalled by Marshal(). Should be <= math.MaxUint32.
	defaultMaxSliceLength = 1 << 18

	// DefaultTagName that enables serialization.
	DefaultTagName = "serialize"

	// TagValue is the value the tag must have to be serialized.
	TagValue = "true"
)

var (
	errMarshalNil   = errors.New("can't marshal nil pointer or interface")
	errUnmarshalNil = errors.New("can't unmarshal nil")
	errNeedPointer  = errors.New("argument to unmarshal must be a pointer")
)

// Codec marshals and unmarshals
type Codec interface {
	codec.Registry
	codec.Codec
	SkipRegistations(int)
}

// Codec handles marshaling and unmarshaling of structs
type linearCodec struct {
	lock         sync.RWMutex
	tagName      string
	maxSliceLen  int
	nextTypeID   uint32
	typeIDToType map[uint32]reflect.Type
	typeToTypeID map[reflect.Type]uint32

	fieldLock sync.Mutex
	// Key: a struct type
	// Value: Slice where each element is index in the struct type
	// of a field that is serialized/deserialized
	// e.g. Foo --> [1,5,8] means Foo.Field(1), etc. are to be serialized/deserialized
	// We assume this cache is pretty small (a few hundred keys at most)
	// and doesn't take up much memory
	serializedFieldIndices map[reflect.Type][]int
}

// New returns a new, concurrency-safe codec
func New(tagName string, maxSliceLen int) Codec {
	return &linearCodec{
		tagName:                tagName,
		maxSliceLen:            maxSliceLen,
		nextTypeID:             0,
		typeIDToType:           map[uint32]reflect.Type{},
		typeToTypeID:           map[reflect.Type]uint32{},
		serializedFieldIndices: map[reflect.Type][]int{},
	}
}

// NewDefault returns a new codec with reasonable default values
func NewDefault() Codec { return New(DefaultTagName, defaultMaxSliceLength) }

// Skip some number of type IDs
func (c *linearCodec) SkipRegistations(num int) {
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
	if _, exists := c.typeToTypeID[valType]; exists {
		return fmt.Errorf("type %v has already been registered", valType)
	}

	c.typeIDToType[c.nextTypeID] = reflect.TypeOf(val)
	c.typeToTypeID[valType] = c.nextTypeID
	c.nextTypeID++
	return nil
}

// A few notes:
// 1) See codec_test.go for examples of usage
// 2) We use "marshal" and "serialize" interchangeably, and "unmarshal" and "deserialize" interchangeably
// 3) To include a field of a struct in the serialized form, add the tag `{tagName}:"true"` to it. `{tagName}` defaults to `serialize`.
// 4) These typed members of a struct may be serialized:
//    bool, string, uint[8,16,32,64], int[8,16,32,64],
//	  structs, slices, arrays, interface.
//	  structs, slices and arrays can only be serialized if their constituent values can be.
// 5) To marshal an interface, you must pass a pointer to the value
// 6) To unmarshal an interface,  you must call codec.RegisterType([instance of the type that fulfills the interface]).
// 7) Serialized fields must be exported
// 8) nil slices are marshaled as empty slices

// To marshal an interface, [value] must be a pointer to the interface
func (c *linearCodec) MarshalInto(value interface{}, p *wrappers.Packer) error {
	if value == nil {
		return errMarshalNil // can't marshal nil
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.marshal(reflect.ValueOf(value), p)
}

// marshal writes the byte representation of [value] to [p]
// [value]'s underlying value must not be a nil pointer or interface
// c.lock should be held for the duration of this function
func (c *linearCodec) marshal(value reflect.Value, p *wrappers.Packer) error {
	valueKind := value.Kind()
	switch valueKind {
	case reflect.Interface, reflect.Ptr, reflect.Invalid:
		if value.IsNil() { // Can't marshal nil (except nil slices)
			return errMarshalNil
		}
	}

	switch valueKind {
	case reflect.Uint8:
		p.PackByte(uint8(value.Uint()))
		return p.Err
	case reflect.Int8:
		p.PackByte(uint8(value.Int()))
		return p.Err
	case reflect.Uint16:
		p.PackShort(uint16(value.Uint()))
		return p.Err
	case reflect.Int16:
		p.PackShort(uint16(value.Int()))
		return p.Err
	case reflect.Uint32:
		p.PackInt(uint32(value.Uint()))
		return p.Err
	case reflect.Int32:
		p.PackInt(uint32(value.Int()))
		return p.Err
	case reflect.Uint64:
		p.PackLong(value.Uint())
		return p.Err
	case reflect.Int64:
		p.PackLong(uint64(value.Int()))
		return p.Err
	case reflect.String:
		p.PackStr(value.String())
		return p.Err
	case reflect.Bool:
		p.PackBool(value.Bool())
		return p.Err
	case reflect.Uintptr, reflect.Ptr:
		return c.marshal(value.Elem(), p)
	case reflect.Interface:
		underlyingValue := value.Interface()
		typeID, ok := c.typeToTypeID[reflect.TypeOf(underlyingValue)] // Get the type ID of the value being marshaled
		if !ok {
			return fmt.Errorf("can't marshal unregistered type '%v'", reflect.TypeOf(underlyingValue).String())
		}
		p.PackInt(typeID) // Pack type ID so we know what to unmarshal this into
		if p.Err != nil {
			return p.Err
		}
		if err := c.marshal(value.Elem(), p); err != nil {
			return err
		}
		return p.Err
	case reflect.Slice:
		numElts := value.Len() // # elements in the slice/array. 0 if this slice is nil.
		if numElts > c.maxSliceLen {
			return fmt.Errorf("slice length, %d, exceeds maximum length, %d", numElts, c.maxSliceLen)
		}
		p.PackInt(uint32(numElts)) // pack # elements
		if p.Err != nil {
			return p.Err
		}
		// If this is a slice of bytes, manually pack the bytes rather
		// than calling marshal on each byte. This improves performance.
		if elemKind := value.Type().Elem().Kind(); elemKind == reflect.Uint8 {
			p.PackFixedBytes(value.Bytes())
			return p.Err
		}
		for i := 0; i < numElts; i++ { // Process each element in the slice
			if err := c.marshal(value.Index(i), p); err != nil {
				return err
			}
		}
		return nil
	case reflect.Array:
		numElts := value.Len()
		if elemKind := value.Type().Kind(); elemKind == reflect.Uint8 {
			sliceVal := value.Convert(reflect.TypeOf([]byte{}))
			p.PackFixedBytes(sliceVal.Bytes())
			return p.Err
		}
		if numElts > c.maxSliceLen {
			return fmt.Errorf("array length, %d, exceeds maximum length, %d", numElts, c.maxSliceLen)
		}
		for i := 0; i < numElts; i++ { // Process each element in the array
			if err := c.marshal(value.Index(i), p); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		serializedFields, err := c.getSerializedFieldIndices(value.Type())
		if err != nil {
			return err
		}
		for _, fieldIndex := range serializedFields { // Go through all fields of this struct that are serialized
			if err := c.marshal(value.Field(fieldIndex), p); err != nil { // Serialize the field and write to byte array
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("can't marshal unknown kind %s", valueKind)
	}
}

// Unmarshal unmarshals [bytes] into [dest], where
// [dest] must be a pointer or interface
func (c *linearCodec) Unmarshal(bytes []byte, dest interface{}) error {
	if dest == nil {
		return errUnmarshalNil
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	p := wrappers.Packer{
		Bytes: bytes,
	}
	destPtr := reflect.ValueOf(dest)
	if destPtr.Kind() != reflect.Ptr {
		return errNeedPointer
	}
	return c.unmarshal(&p, destPtr.Elem())
}

// Unmarshal from p.Bytes into [value]. [value] must be addressable.
// c.lock should be held for the duration of this function
func (c *linearCodec) unmarshal(p *wrappers.Packer, value reflect.Value) error {
	switch value.Kind() {
	case reflect.Uint8:
		value.SetUint(uint64(p.UnpackByte()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint8: %w", p.Err)
		}
		return nil
	case reflect.Int8:
		value.SetInt(int64(p.UnpackByte()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int8: %w", p.Err)
		}
		return nil
	case reflect.Uint16:
		value.SetUint(uint64(p.UnpackShort()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint16: %w", p.Err)
		}
		return nil
	case reflect.Int16:
		value.SetInt(int64(p.UnpackShort()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int16: %w", p.Err)
		}
		return nil
	case reflect.Uint32:
		value.SetUint(uint64(p.UnpackInt()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint32: %w", p.Err)
		}
		return nil
	case reflect.Int32:
		value.SetInt(int64(p.UnpackInt()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int32: %w", p.Err)
		}
		return nil
	case reflect.Uint64:
		value.SetUint(p.UnpackLong())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint64: %w", p.Err)
		}
		return nil
	case reflect.Int64:
		value.SetInt(int64(p.UnpackLong()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int64: %w", p.Err)
		}
		return nil
	case reflect.Bool:
		value.SetBool(p.UnpackBool())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal bool: %w", p.Err)
		}
		return nil
	case reflect.Slice:
		numElts := int(p.UnpackInt())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal slice: %w", p.Err)
		}
		if numElts > c.maxSliceLen {
			return fmt.Errorf("array length, %d, exceeds maximum length, %d",
				numElts, c.maxSliceLen)
		}
		// If this is a slice of bytes, manually unpack the bytes rather
		// than calling unmarshal on each byte. This improves performance.
		if elemKind := value.Type().Elem().Kind(); elemKind == reflect.Uint8 {
			value.SetBytes(p.UnpackFixedBytes(numElts))
			return p.Err
		}
		// set [value] to be a slice of the appropriate type/capacity (right now it is nil)
		value.Set(reflect.MakeSlice(value.Type(), numElts, numElts))
		// Unmarshal each element into the appropriate index of the slice
		for i := 0; i < numElts; i++ {
			if err := c.unmarshal(p, value.Index(i)); err != nil {
				return fmt.Errorf("couldn't unmarshal slice element: %w", err)
			}
		}
		return nil
	case reflect.Array:
		numElts := value.Len()
		if elemKind := value.Type().Elem().Kind(); elemKind == reflect.Uint8 {
			unpackedBytes := p.UnpackFixedBytes(numElts)
			if p.Errored() {
				return p.Err
			}
			// Get a slice to the underlying array value
			underlyingSlice := value.Slice(0, numElts).Interface().([]byte)
			copy(underlyingSlice, unpackedBytes)
			return nil
		}
		for i := 0; i < numElts; i++ {
			if err := c.unmarshal(p, value.Index(i)); err != nil {
				return fmt.Errorf("couldn't unmarshal array element: %w", err)
			}
		}
		return nil
	case reflect.String:
		value.SetString(p.UnpackStr())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal string: %w", p.Err)
		}
		return nil
	case reflect.Interface:
		typeID := p.UnpackInt() // Get the type ID
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal interface: %w", p.Err)
		}
		// Get a type that implements the interface
		implementingType, ok := c.typeIDToType[typeID]
		if !ok {
			return fmt.Errorf("couldn't unmarshal interface: unknown type ID %d", typeID)
		}
		// Ensure type actually does implement the interface
		if valueType := value.Type(); !implementingType.Implements(valueType) {
			return fmt.Errorf("couldn't unmarshal interface: %s does not implement interface %s", implementingType, valueType)
		}
		intfImplementor := reflect.New(implementingType).Elem() // instance of the proper type
		// Unmarshal into the struct
		if err := c.unmarshal(p, intfImplementor); err != nil {
			return fmt.Errorf("couldn't unmarshal interface: %w", err)
		}
		// And assign the filled struct to the value
		value.Set(intfImplementor)
		return nil
	case reflect.Struct:
		// Get indices of fields that will be unmarshaled into
		serializedFieldIndices, err := c.getSerializedFieldIndices(value.Type())
		if err != nil {
			return fmt.Errorf("couldn't unmarshal struct: %w", err)
		}
		// Go through the fields and umarshal into them
		for _, index := range serializedFieldIndices {
			if err := c.unmarshal(p, value.Field(index)); err != nil {
				return fmt.Errorf("couldn't unmarshal struct: %w", err)
			}
		}
		return nil
	case reflect.Ptr:
		// Get the type this pointer points to
		t := value.Type().Elem()
		// Create a new pointer to a new value of the underlying type
		v := reflect.New(t)
		// Fill the value
		if err := c.unmarshal(p, v.Elem()); err != nil {
			return fmt.Errorf("couldn't unmarshal pointer: %w", err)
		}
		// Assign to the top-level struct's member
		value.Set(v)
		return nil
	case reflect.Invalid:
		return errUnmarshalNil
	default:
		return fmt.Errorf("can't unmarshal unknown type %s", value.Kind().String())
	}
}

// Returns the indices of the serializable fields of [t], which is a struct type
// Returns an error if a field has tag "serialize: true" but the field is unexported
// e.g. getSerializedFieldIndices(Foo) --> [1,5,8] means Foo.Field(1), Foo.Field(5), Foo.Field(8)
// are to be serialized/deserialized
// c.lock should be held for the duration of this method
func (c *linearCodec) getSerializedFieldIndices(t reflect.Type) ([]int, error) {
	c.fieldLock.Lock()
	defer c.fieldLock.Unlock()

	if c.serializedFieldIndices == nil {
		c.serializedFieldIndices = make(map[reflect.Type][]int)
	}
	if serializedFields, ok := c.serializedFieldIndices[t]; ok { // use pre-computed result
		return serializedFields, nil
	}
	numFields := t.NumField()
	serializedFields := make([]int, 0, numFields)
	for i := 0; i < numFields; i++ { // Go through all fields of this struct
		field := t.Field(i)
		if field.Tag.Get(c.tagName) != TagValue { // Skip fields we don't need to serialize
			continue
		}
		if unicode.IsLower(rune(field.Name[0])) { // Can only marshal exported fields
			return []int{}, fmt.Errorf("can't marshal unexported field %s", field.Name)
		}
		serializedFields = append(serializedFields, i)
	}
	c.serializedFieldIndices[t] = serializedFields // cache result
	return serializedFields, nil
}
