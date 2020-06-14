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

	funcs := make([]func(*wrappers.Packer) error, 512, 512)
	size, _, err := c.marshal(reflect.ValueOf(value), 0, &funcs)
	if err != nil {
		return nil, err
	}

	p := &wrappers.Packer{MaxSize: size, Bytes: make([]byte, 0, size)}
	for _, f := range funcs {
		if f == nil {
			break
		} else if err := f(p); err != nil {
			return nil, err
		}
	}

	return p.Bytes, nil
}

// marshal returns:
// 1) The size, in bytes, of the byte representation of [value]
// 2) A slice of functions, where each function writes bytes to its argument
//    and returns the number of bytes it wrote.
//    When these functions are called in order, they write [value] to a byte slice.
// 3) An error
func (c *codec) marshal(value reflect.Value, index int, funcs *[]func(*wrappers.Packer) error) (size int, funcsWritten int, err error) {
	valueKind := value.Kind()

	// Case: Value can't be marshalled
	switch valueKind {
	case reflect.Interface, reflect.Ptr, reflect.Invalid:
		if value.IsNil() { // Can't marshal nil or nil pointers
			return 0, 0, errNil
		}
	}

	// Case: Value is of known size; return its byte repr.
	switch valueKind {
	case reflect.Uint8:
		size = 1
		funcsWritten = 1
		asByte := byte(value.Uint())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackByte(asByte)
			return p.Err
		}
		return
	case reflect.Int8:
		size = 1
		funcsWritten = 1
		asByte := byte(value.Int())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackByte(asByte)
			return p.Err
		}
		return
	case reflect.Uint16:
		size = 2
		funcsWritten = 1
		asShort := uint16(value.Uint())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackShort(asShort)
			return p.Err
		}
		return
	case reflect.Int16:
		size = 2
		funcsWritten = 1
		asShort := uint16(value.Int())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackShort(asShort)
			return p.Err
		}
		return
	case reflect.Uint32:
		size = 4
		funcsWritten = 1
		asInt := uint32(value.Uint())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackInt(asInt)
			return p.Err
		}
		return
	case reflect.Int32:
		size = 4
		funcsWritten = 1
		asInt := uint32(value.Int())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackInt(asInt)
			return p.Err
		}
		return
	case reflect.Uint64:
		size = 8
		funcsWritten = 1
		asInt := uint64(value.Uint())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackLong(asInt)
			return p.Err
		}
		return
	case reflect.Int64:
		size = 8
		funcsWritten = 1
		asInt := uint64(value.Int())
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackLong(asInt)
			return p.Err
		}
		return
	case reflect.String:
		funcsWritten = 1
		asStr := value.String()
		size = len(asStr) + wrappers.ShortLen
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackStr(asStr)
			return p.Err
		}
		return
	case reflect.Bool:
		size = 1
		funcsWritten = 1
		asBool := value.Bool()
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackBool(asBool)
			return p.Err
		}
		return
	case reflect.Uintptr, reflect.Ptr:
		return c.marshal(value.Elem(), index, funcs)
	case reflect.Interface:
		underlyingValue := value.Interface()
		typeID, ok := c.typeToTypeID[reflect.TypeOf(underlyingValue)] // Get the type ID of the value being marshaled
		if !ok {
			return 0, 0, fmt.Errorf("can't marshal unregistered type '%v'", reflect.TypeOf(underlyingValue).String())
		}

		(*funcs)[index] = nil
		subsize, subFuncsWritten, subErr := c.marshal(value.Elem(), index+1, funcs)
		if subErr != nil {
			return 0, 0, subErr
		}

		size = 4 + subsize // 4 because we pack the type ID, a uint32
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackInt(typeID)
			return p.Err
		}
		funcsWritten = 1 + subFuncsWritten
		return
	case reflect.Slice:
		numElts := value.Len() // # elements in the slice/array. 0 if this slice is nil.
		if numElts > c.maxSliceLen {
			return 0, 0, fmt.Errorf("slice length, %d, exceeds maximum length, %d", numElts, c.maxSliceLen)
		}

		size = wrappers.IntLen // for # elements
		subFuncsWritten := 0
		for i := 0; i < numElts; i++ { // Process each element in the slice
			subSize, n, subErr := c.marshal(value.Index(i), index+subFuncsWritten+1, funcs)
			if subErr != nil {
				return 0, 0, subErr
			}
			size += subSize
			subFuncsWritten += n
		}

		numEltsAsUint32 := uint32(numElts)
		(*funcs)[index] = func(p *wrappers.Packer) error {
			p.PackInt(numEltsAsUint32) // pack # elements
			return p.Err
		}
		funcsWritten = subFuncsWritten + 1
		return
	case reflect.Array:
		numElts := value.Len()
		if numElts > c.maxSliceLen {
			return 0, 0, fmt.Errorf("array length, %d, exceeds maximum length, %d", numElts, c.maxSliceLen)
		}

		size = 0
		funcsWritten = 0
		for i := 0; i < numElts; i++ { // Process each element in the array
			subSize, n, subErr := c.marshal(value.Index(i), index+funcsWritten, funcs)
			if subErr != nil {
				return 0, 0, subErr
			}
			size += subSize
			funcsWritten += n
		}
		return
	case reflect.Struct:
		t := value.Type()

		size = 0
		fieldsMarshalled := 0
		funcsWritten = 0
		serializedFields, subErr := c.getSerializedFieldIndices(t)
		if subErr != nil {
			return 0, 0, subErr
		}

		for _, f := range serializedFields { // Go through all fields of this struct
			fieldVal := value.Field(f)                                        // The field we're serializing
			subSize, n, err := c.marshal(fieldVal, index+funcsWritten, funcs) // Serialize the field
			if err != nil {
				return 0, 0, err
			}
			fieldsMarshalled++
			size += subSize
			funcsWritten += n
		}
		return
	default:
		return 0, 0, errUnknownType
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
