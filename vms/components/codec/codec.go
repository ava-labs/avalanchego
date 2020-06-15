// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"unicode"

	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	defaultMaxSize        = 1 << 18 // default max size, in bytes, of something being marshalled by Marshal()
	defaultMaxSliceLength = 1 << 18 // default max length of a slice being marshalled by Marshal(). Should be <= math.MaxUint32.
	maxStringLen          = math.MaxUint16
)

// ErrBadCodec is returned when one tries to perform an operation
// using an unknown codec
var (
	errBadCodec                  = errors.New("wrong or unknown codec used")
	errNil                       = errors.New("can't marshal/unmarshal nil value")
	errNeedPointer               = errors.New("must unmarshal into a pointer")
	errMarshalUnregisteredType   = errors.New("can't marshal an unregistered type")
	errUnmarshalUnregisteredType = errors.New("can't unmarshal an unregistered type")
	errUnknownType               = errors.New("don't know how to marshal/unmarshal this type")
	errMarshalUnexportedField    = errors.New("can't serialize an unexported field")
	errUnmarshalUnexportedField  = errors.New("can't deserialize into an unexported field")
	errOutOfMemory               = errors.New("out of memory")
	errSliceTooLarge             = errors.New("slice too large")
)

// Codec handles marshaling and unmarshaling of structs
type codec struct {
	maxSize     int
	maxSliceLen int

	typeIDToType map[uint32]reflect.Type
	typeToTypeID map[reflect.Type]uint32

	serializedFieldIndices map[reflect.Type][]int
}

// Codec marshals and unmarshals
type Codec interface {
	RegisterType(interface{}) error
	Marshal(interface{}) ([]byte, error)
	Unmarshal([]byte, interface{}) error
}

// New returns a new codec
func New(maxSize, maxSliceLen int) Codec {
	return &codec{
		maxSize:                maxSize,
		maxSliceLen:            maxSliceLen,
		typeIDToType:           map[uint32]reflect.Type{},
		typeToTypeID:           map[reflect.Type]uint32{},
		serializedFieldIndices: map[reflect.Type][]int{},
	}
}

// NewDefault returns a new codec with reasonable default values
func NewDefault() Codec { return New(defaultMaxSize, defaultMaxSliceLength) }

// RegisterType is used to register types that may be unmarshaled into an interface
// [val] is a value of the type being registered
func (c *codec) RegisterType(val interface{}) error {
	valType := reflect.TypeOf(val)
	if _, exists := c.typeToTypeID[valType]; exists {
		return fmt.Errorf("type %v has already been registered", valType)
	}
	c.typeIDToType[uint32(len(c.typeIDToType))] = reflect.TypeOf(val)
	c.typeToTypeID[valType] = uint32(len(c.typeIDToType) - 1)
	return nil
}

// A few notes:
// 1) See codec_test.go for examples of usage
// 2) We use "marshal" and "serialize" interchangeably, and "unmarshal" and "deserialize" interchangeably
// 3) To include a field of a struct in the serialized form, add the tag `serialize:"true"` to it
// 4) These typed members of a struct may be serialized:
//    bool, string, uint[8,16,32,64], int[8,16,32,64],
//	  structs, slices, arrays, interface.
//	  structs, slices and arrays can only be serialized if their constituent values can be.
// 5) To marshal an interface, you must pass a pointer to the value
// 6) To unmarshal an interface,  you must call codec.RegisterType([instance of the type that fulfills the interface]).
// 7) Serialized fields must be exported
// 8) nil slices are marshaled as empty slices

// To marshal an interface, [value] must be a pointer to the interface
func (c *codec) Marshal(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, errNil
	}

	p := &wrappers.Packer{MaxSize: 512, Bytes: make([]byte, 0, 512)}
	if err := c.marshal(reflect.ValueOf(value), p); err != nil {
		return nil, err
	}

	return p.Bytes, nil
}

// marshal returns:
// 1) The size, in bytes, of the byte representation of [value]
// 2) A slice of functions, where each function writes bytes to its argument
//    and returns the number of bytes it wrote.
//    When these functions are called in order, they write [value] to a byte slice.
// 3) An error
func (c *codec) marshal(value reflect.Value, p *wrappers.Packer) error {
	valueKind := value.Kind()

	// Case: Value can't be marshalled
	switch valueKind {
	case reflect.Interface, reflect.Ptr, reflect.Invalid:
		if value.IsNil() { // Can't marshal nil (except nil slices)
			return errNil
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
		p.PackInt(typeID)
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

		for i := 0; i < numElts; i++ { // Process each element in the slice
			if err := c.marshal(value.Index(i), p); err != nil {
				return err
			}
		}

		return nil
	case reflect.Array:
		numElts := value.Len()
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
		serializedFields, subErr := c.getSerializedFieldIndices(value.Type())
		if subErr != nil {
			return subErr
		}

		for _, fieldIndex := range serializedFields { // Go through all fields of this struct
			if err := c.marshal(value.Field(fieldIndex), p); err != nil { // Serialize the field
				return err
			}
		}
		return nil
	default:
		return errUnknownType
	}
}

// Unmarshal unmarshals [bytes] into [dest], where
// [dest] must be a pointer or interface
func (c *codec) Unmarshal(bytes []byte, dest interface{}) error {
	switch {
	case len(bytes) > c.maxSize:
		return errSliceTooLarge
	case dest == nil:
		return errNil
	}

	destPtr := reflect.ValueOf(dest)
	if destPtr.Kind() != reflect.Ptr {
		return errNeedPointer
	}

	p := &wrappers.Packer{MaxSize: c.maxSize, Bytes: bytes}
	destVal := destPtr.Elem()
	if err := c.unmarshal(p, destVal); err != nil {
		return err
	}

	return nil
}

// Unmarshal from [bytes] into [value]. [value] must be addressable
func (c *codec) unmarshal(p *wrappers.Packer, value reflect.Value) error {
	switch value.Kind() {
	case reflect.Uint8:
		value.SetUint(uint64(p.UnpackByte()))
		return p.Err
	case reflect.Int8:
		value.SetInt(int64(p.UnpackByte()))
		return p.Err
	case reflect.Uint16:
		value.SetUint(uint64(p.UnpackShort()))
		return p.Err
	case reflect.Int16:
		value.SetInt(int64(p.UnpackShort()))
		return p.Err
	case reflect.Uint32:
		value.SetUint(uint64(p.UnpackInt()))
		return p.Err
	case reflect.Int32:
		value.SetInt(int64(p.UnpackInt()))
		return p.Err
	case reflect.Uint64:
		value.SetUint(uint64(p.UnpackLong()))
		return p.Err
	case reflect.Int64:
		value.SetInt(int64(p.UnpackLong()))
		return p.Err
	case reflect.Bool:
		value.SetBool(p.UnpackBool())
		return p.Err
	case reflect.Slice:
		numElts := int(p.UnpackInt())
		if p.Err != nil {
			return p.Err
		}
		// set [value] to be a slice of the appropriate type/capacity (right now [value] is nil)
		value.Set(reflect.MakeSlice(value.Type(), numElts, numElts))
		// Unmarshal each element into the appropriate index of the slice
		for i := 0; i < numElts; i++ {
			if err := c.unmarshal(p, value.Index(i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := c.unmarshal(p, value.Index(i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.String:
		value.SetString(p.UnpackStr())
		return p.Err
	case reflect.Interface:
		typeID := p.UnpackInt() // Get the type ID
		if p.Err != nil {
			return p.Err
		}
		// Get a type that implements the interface
		typ, ok := c.typeIDToType[typeID]
		if !ok {
			return errUnmarshalUnregisteredType
		}
		// Ensure type actually does implement the interface
		if valueType := value.Type(); !typ.Implements(valueType) {
			return fmt.Errorf("%s does not implement interface %s", typ, valueType)
		}
		concreteInstance := reflect.New(typ).Elem() // instance of the proper type
		// Unmarshal into the struct
		if err := c.unmarshal(p, concreteInstance); err != nil {
			return err
		}
		// And assign the filled struct to the value
		value.Set(concreteInstance)
		return nil
	case reflect.Struct:
		// Type of this struct
		serializedFieldIndices, err := c.getSerializedFieldIndices(value.Type())
		if err != nil {
			return err
		}
		// Go through all the fields and umarshal into each
		for _, index := range serializedFieldIndices {
			if err := c.unmarshal(p, value.Field(index)); err != nil { // Unmarshal into the field
				return err
			}
		}
		return nil
	case reflect.Ptr:
		// Get the type this pointer points to
		underlyingType := value.Type().Elem()
		// Create a new pointer to a new value of the underlying type
		underlyingValue := reflect.New(underlyingType)
		// Fill the value
		if err := c.unmarshal(p, underlyingValue.Elem()); err != nil {
			return err
		}
		// Assign to the top-level struct's member
		value.Set(underlyingValue)
		return nil
	case reflect.Invalid:
		return errNil
	default:
		return errUnknownType
	}
}

// Returns the indices of the serializable fields of [t], which is a struct type
// Returns an error if a field has tag "serialize: true" but the field is unexported
func (c *codec) getSerializedFieldIndices(t reflect.Type) ([]int, error) {
	if c.serializedFieldIndices == nil {
		c.serializedFieldIndices = make(map[reflect.Type][]int)
	}
	if serializedFields, ok := c.serializedFieldIndices[t]; ok {
		return serializedFields, nil
	}
	numFields := t.NumField()
	serializedFields := make([]int, 0, numFields)
	for i := 0; i < numFields; i++ { // Go through all fields of this struct
		field := t.Field(i)
		if field.Tag.Get("serialize") != "true" { // Skip fields we don't need to serialize
			continue
		}
		if unicode.IsLower(rune(field.Name[0])) { // Can only marshal exported fields
			return []int{}, fmt.Errorf("can't marshal unexported field %s", field.Name)
		}
		serializedFields = append(serializedFields, i)
	}
	c.serializedFieldIndices[t] = serializedFields
	return serializedFields, nil
}
