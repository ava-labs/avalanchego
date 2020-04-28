// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"errors"
	"fmt"
	"reflect"
	"unicode"

	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	defaultMaxSize        = 1 << 18 // default max size, in bytes, of something being marshalled by Marshal()
	defaultMaxSliceLength = 1 << 18 // default max length of a slice being marshalled by Marshal()
)

// ErrBadCodec is returned when one tries to perform an operation
// using an unknown codec
var (
	errBadCodec                  = errors.New("wrong or unknown codec used")
	errNil                       = errors.New("can't marshal nil value")
	errUnmarshalNil              = errors.New("can't unmarshal into nil")
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
}

// Codec marshals and unmarshals
type Codec interface {
	RegisterType(interface{}) error
	Marshal(interface{}) ([]byte, error)
	Unmarshal([]byte, interface{}) error
}

// New returns a new codec
func New(maxSize, maxSliceLen int) Codec {
	return codec{
		maxSize:      maxSize,
		maxSliceLen:  maxSliceLen,
		typeIDToType: map[uint32]reflect.Type{},
		typeToTypeID: map[reflect.Type]uint32{},
	}
}

// NewDefault returns a new codec with reasonable default values
func NewDefault() Codec { return New(defaultMaxSize, defaultMaxSliceLength) }

// RegisterType is used to register types that may be unmarshaled into an interface typed value
// [val] is a value of the type being registered
func (c codec) RegisterType(val interface{}) error {
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
//    bool, string, uint[8,16,32,64, int[8,16,32,64],
//	  structs, slices, arrays, interface.
//	  structs, slices and arrays can only be serialized if their constituent parts can be.
// 5) To marshal an interface typed value, you must pass a _pointer_ to the value
// 6) If you want to be able to unmarshal into an interface typed value,
//    you must call codec.RegisterType([instance of the type that fulfills the interface]).
// 7) nil slices will be unmarshaled as an empty slice of the appropriate type
// 8) Serialized fields must be exported

// Marshal returns the byte representation of [value]
// If you want to marshal an interface, [value] must be a pointer
// to the interface
func (c codec) Marshal(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, errNil
	}

	return c.marshal(reflect.ValueOf(value))
}

// Marshal [value] to bytes
func (c codec) marshal(value reflect.Value) ([]byte, error) {
	p := wrappers.Packer{MaxSize: c.maxSize, Bytes: []byte{}}
	t := value.Type()

	valueKind := value.Kind()
	switch valueKind {
	case reflect.Interface, reflect.Ptr, reflect.Slice:
		if value.IsNil() {
			return nil, errNil
		}
	}

	switch valueKind {
	case reflect.Uint8:
		p.PackByte(uint8(value.Uint()))
		return p.Bytes, p.Err
	case reflect.Int8:
		p.PackByte(uint8(value.Int()))
		return p.Bytes, p.Err
	case reflect.Uint16:
		p.PackShort(uint16(value.Uint()))
		return p.Bytes, p.Err
	case reflect.Int16:
		p.PackShort(uint16(value.Int()))
		return p.Bytes, p.Err
	case reflect.Uint32:
		p.PackInt(uint32(value.Uint()))
		return p.Bytes, p.Err
	case reflect.Int32:
		p.PackInt(uint32(value.Int()))
		return p.Bytes, p.Err
	case reflect.Uint64:
		p.PackLong(value.Uint())
		return p.Bytes, p.Err
	case reflect.Int64:
		p.PackLong(uint64(value.Int()))
		return p.Bytes, p.Err
	case reflect.Uintptr, reflect.Ptr:
		return c.marshal(value.Elem())
	case reflect.String:
		p.PackStr(value.String())
		return p.Bytes, p.Err
	case reflect.Bool:
		p.PackBool(value.Bool())
		return p.Bytes, p.Err
	case reflect.Interface:
		typeID, ok := c.typeToTypeID[reflect.TypeOf(value.Interface())] // Get the type ID of the value being marshaled
		if !ok {
			return nil, fmt.Errorf("can't marshal unregistered type '%v'", reflect.TypeOf(value.Interface()).String())
		}
		p.PackInt(typeID)
		bytes, err := c.Marshal(value.Interface())
		if err != nil {
			return nil, err
		}
		p.PackFixedBytes(bytes)
		if p.Errored() {
			return nil, p.Err
		}
		return p.Bytes, err
	case reflect.Array, reflect.Slice:
		numElts := value.Len() // # elements in the slice/array (assumed to be <= 2^31 - 1)
		// If this is a slice, pack the number of elements in the slice
		if valueKind == reflect.Slice {
			p.PackInt(uint32(numElts))
		}
		for i := 0; i < numElts; i++ { // Pack each element in the slice/array
			eltBytes, err := c.marshal(value.Index(i))
			if err != nil {
				return nil, err
			}
			p.PackFixedBytes(eltBytes)
		}
		return p.Bytes, p.Err
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ { // Go through all fields of this struct
			field := t.Field(i)
			if !shouldSerialize(field) { // Skip fields we don't need to serialize
				continue
			}
			if unicode.IsLower(rune(field.Name[0])) { // Can only marshal exported fields
				return nil, errMarshalUnexportedField
			}
			fieldVal := value.Field(i) // The field we're serializing
			if fieldVal.Kind() == reflect.Slice && fieldVal.IsNil() {
				p.PackInt(0)
				continue
			}
			fieldBytes, err := c.marshal(fieldVal) // Serialize the field
			if err != nil {
				return nil, err
			}
			p.PackFixedBytes(fieldBytes)
		}
		return p.Bytes, p.Err
	case reflect.Invalid:
		return nil, errUnmarshalNil
	default:
		return nil, errUnknownType
	}
}

// Unmarshal unmarshals [bytes] into [dest], where
// [dest] must be a pointer or interface
func (c codec) Unmarshal(bytes []byte, dest interface{}) error {
	p := &wrappers.Packer{Bytes: bytes}

	if len(bytes) > c.maxSize {
		return errSliceTooLarge
	}

	if dest == nil {
		return errNil
	}

	destPtr := reflect.ValueOf(dest)

	if destPtr.Kind() != reflect.Ptr {
		return errNeedPointer
	}

	destVal := destPtr.Elem()

	err := c.unmarshal(p, destVal)
	if err != nil {
		return err
	}

	if p.Offset != len(p.Bytes) {
		return fmt.Errorf("has %d leftover bytes after unmarshalling", len(p.Bytes)-p.Offset)
	}
	return nil
}

// Unmarshal bytes from [p] into [field]
// [field] must be addressable
func (c codec) unmarshal(p *wrappers.Packer, field reflect.Value) error {
	kind := field.Kind()
	switch kind {
	case reflect.Uint8:
		field.SetUint(uint64(p.UnpackByte()))
	case reflect.Int8:
		field.SetInt(int64(p.UnpackByte()))
	case reflect.Uint16:
		field.SetUint(uint64(p.UnpackShort()))
	case reflect.Int16:
		field.SetInt(int64(p.UnpackShort()))
	case reflect.Uint32:
		field.SetUint(uint64(p.UnpackInt()))
	case reflect.Int32:
		field.SetInt(int64(p.UnpackInt()))
	case reflect.Uint64:
		field.SetUint(p.UnpackLong())
	case reflect.Int64:
		field.SetInt(int64(p.UnpackLong()))
	case reflect.Bool:
		field.SetBool(p.UnpackBool())
	case reflect.Slice:
		sliceLen := int(p.UnpackInt()) // number of elements in the slice
		if sliceLen < 0 || sliceLen > c.maxSliceLen {
			return errSliceTooLarge
		}

		// First set [field] to be a slice of the appropriate type/capacity (right now [field] is nil)
		slice := reflect.MakeSlice(field.Type(), sliceLen, sliceLen)
		field.Set(slice)
		// Unmarshal each element into the appropriate index of the slice
		for i := 0; i < sliceLen; i++ {
			if err := c.unmarshal(p, field.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := 0; i < field.Len(); i++ {
			if err := c.unmarshal(p, field.Index(i)); err != nil {
				return err
			}
		}
	case reflect.String:
		field.SetString(p.UnpackStr())
	case reflect.Interface:
		// Get the type ID
		typeID := p.UnpackInt()
		// Get a struct that implements the interface
		typ, ok := c.typeIDToType[typeID]
		if !ok {
			return errUnmarshalUnregisteredType
		}
		// Ensure struct actually does implement the interface
		fieldType := field.Type()
		if !typ.Implements(fieldType) {
			return fmt.Errorf("%s does not implement interface %s", typ, fieldType)
		}
		concreteInstancePtr := reflect.New(typ) // instance of the proper type
		// Unmarshal into the struct
		if err := c.unmarshal(p, concreteInstancePtr.Elem()); err != nil {
			return err
		}
		// And assign the filled struct to the field
		field.Set(concreteInstancePtr.Elem())
	case reflect.Struct:
		// Type of this struct
		structType := reflect.TypeOf(field.Interface())
		// Go through all the fields and umarshal into each
		for i := 0; i < structType.NumField(); i++ {
			structField := structType.Field(i)
			if !shouldSerialize(structField) { // Skip fields we don't need to unmarshal
				continue
			}
			if unicode.IsLower(rune(structField.Name[0])) { // Only unmarshal into exported field
				return errUnmarshalUnexportedField
			}
			field := field.Field(i)                       // Get the field
			if err := c.unmarshal(p, field); err != nil { // Unmarshal into the field
				return err
			}
			if p.Errored() { // If there was an error just return immediately
				return p.Err
			}
		}
	case reflect.Ptr:
		// Get the type this pointer points to
		underlyingType := field.Type().Elem()
		// Create a new pointer to a new value of the underlying type
		underlyingValue := reflect.New(underlyingType)
		// Fill the value
		if err := c.unmarshal(p, underlyingValue.Elem()); err != nil {
			return err
		}
		// Assign to the top-level struct's member
		field.Set(underlyingValue)
	case reflect.Invalid:
		return errUnmarshalNil
	default:
		return errUnknownType
	}
	return p.Err
}

// Returns true iff [field] should be serialized
func shouldSerialize(field reflect.StructField) bool {
	return field.Tag.Get("serialize") == "true"
}
