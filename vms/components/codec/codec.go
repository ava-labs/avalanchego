// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"unicode"
)

const (
	defaultMaxSize        = 1 << 18 // default max size, in bytes, of something being marshalled by Marshal()
	defaultMaxSliceLength = 1 << 18 // default max length of a slice being marshalled by Marshal()
	maxStringLen          = math.MaxInt16
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

// RegisterType is used to register types that may be unmarshaled into an interface
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
//    bool, string, uint[8,16,32,64], int[8,16,32,64],
//	  structs, slices, arrays, interface.
//	  structs, slices and arrays can only be serialized if their constituent values can be.
// 5) To marshal an interface, you must pass a pointer to the value
// 6) To unmarshal an interface,  you must call codec.RegisterType([instance of the type that fulfills the interface]).
// 7) nil slices will be unmarshaled as an empty slice of the appropriate type
// 8) Serialized fields must be exported

// To marshal an interface, [value] must be a pointer to the interface
func (c codec) Marshal(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, errNil
	}
	size, f, err := c.marshal(reflect.ValueOf(value))
	if err != nil {
		return nil, err
	}

	bytes := make([]byte, size, size)
	if err := f(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

// marshal returns:
// 1) The size, in bytes, of the byte representation of [value]
// 2) A slice of functions, where each function writes bytes to its argument
//    and returns the number of bytes it wrote.
//    When these functions are called in order, they write [value] to a byte slice.
// 3) An error
func (c codec) marshal(value reflect.Value) (size int, f func([]byte) error, err error) {
	valueKind := value.Kind()

	// Case: Value can't be marshalled
	switch valueKind {
	case reflect.Interface, reflect.Ptr, reflect.Invalid:
		if value.IsNil() { // Can't marshal nil or nil pointers
			return 0, nil, errNil
		}
	}

	// Case: Value is of known size; return its byte repr.
	switch valueKind {
	case reflect.Uint8:
		size = 1
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 1 {
				return fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
			}
			copy(b, []byte{byte(value.Uint())})
			return nil
		}
		return
	case reflect.Int8:
		size = 1
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 1 {
				return fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
			}
			copy(b, []byte{byte(value.Int())})
			return nil
		}
		return
	case reflect.Uint16:
		size = 2
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 2 {
				return fmt.Errorf("expected len(bytes) to be at least 2 but is %d", bytesLen)
			}
			binary.BigEndian.PutUint16(b, uint16(value.Uint()))
			return nil
		}
		return
	case reflect.Int16:
		size = 2
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 2 {
				return fmt.Errorf("expected len(bytes) to be at least 2 but is %d", bytesLen)
			}
			binary.BigEndian.PutUint16(b, uint16(value.Int()))
			return nil
		}
		return
	case reflect.Uint32:
		size = 4
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 4 {
				return fmt.Errorf("expected len(bytes) to be at least 4 but is %d", bytesLen)
			}
			binary.BigEndian.PutUint32(b, uint32(value.Uint()))
			return nil
		}
		return
	case reflect.Int32:
		size = 4
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 4 {
				return fmt.Errorf("expected len(bytes) to be at least 4 but is %d", bytesLen)
			}
			binary.BigEndian.PutUint32(b, uint32(value.Int()))
			return nil
		}
		return
	case reflect.Uint64:
		size = 8
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 8 {
				return fmt.Errorf("expected len(bytes) to be at least 8 but is %d", bytesLen)
			}
			binary.BigEndian.PutUint64(b, uint64(value.Uint()))
			return nil
		}
		return
	case reflect.Int64:
		size = 8
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 8 {
				return fmt.Errorf("expected len(bytes) to be at least 8 but is %d", bytesLen)
			}
			binary.BigEndian.PutUint64(b, uint64(value.Int()))
			return nil
		}
		return
	case reflect.String:
		asStr := value.String()
		strSize := len(asStr)
		if strSize > maxStringLen {
			return 0, nil, errSliceTooLarge
		}

		size = strSize + 2
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < size {
				return fmt.Errorf("expected len(bytes) to be at least %d but is %d", size, bytesLen)
			}
			binary.BigEndian.PutUint16(b, uint16(strSize))
			if strSize == 0 {
				return nil
			}
			copy(b[2:], []byte(asStr))
			return nil
		}
		return
	case reflect.Bool:
		size = 1
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 1 {
				return fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
			}
			if value.Bool() {
				copy(b, []byte{1})
			} else {
				copy(b, []byte{0})
			}
			return nil
		}
		return
	case reflect.Uintptr, reflect.Ptr:
		return c.marshal(value.Elem())
	case reflect.Interface:
		typeID, ok := c.typeToTypeID[reflect.TypeOf(value.Interface())] // Get the type ID of the value being marshaled
		if !ok {
			return 0, nil, fmt.Errorf("can't marshal unregistered type '%v'", reflect.TypeOf(value.Interface()).String())
		}

		subsize, subfunc, subErr := c.marshal(reflect.ValueOf(value.Interface())) // TODO: Is this right?
		if subErr != nil {
			return 0, nil, subErr
		}

		size = 4 + subsize // 4 because we pack the type ID, a uint32
		f = func(b []byte) error {
			if bytesLen := len(b); bytesLen < 4+subsize {
				return fmt.Errorf("expected len(bytes) to be at least %d but is %d", 4+subsize, bytesLen)
			}
			binary.BigEndian.PutUint32(b, uint32(typeID))
			if len(b) == 4 {
				return nil
			}
			return subfunc(b[4:])
		}
		return
	case reflect.Slice:
		if value.IsNil() {
			size = 1
			f = func(b []byte) error {
				if bytesLen := len(b); bytesLen < 1 {
					return fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
				}
				b[0] = 1 // slice is nil; set isNil flag to 1
				return nil
			}
			return
		}

		numElts := value.Len() // # elements in the slice/array (assumed to be <= 2^31 - 1)
		if numElts > c.maxSliceLen {
			return 0, nil, fmt.Errorf("slice length, %d, exceeds maximum length, %d", numElts, math.MaxUint32)
		}

		size = 5 // 1 for the isNil flag. 0 --> this slice isn't nil. 1--> it is nil.
		// 4 for the size of the slice (uint32)

		// offsets[i] is the index in the byte array that subFuncs[i] will start writing at
		offsets := make([]int, numElts+1, numElts+1)
		if numElts != 0 {
			offsets[1] = 5 // 1 for nil flag, 4 for slice size
		}
		subFuncs := make([]func([]byte) error, numElts+1, numElts+1)
		subFuncs[0] = func(b []byte) error { // write the nil flag and number of elements
			if bytesLen := len(b); bytesLen < 5 {
				return fmt.Errorf("expected len(bytes) to be at least 5 but is %d", bytesLen)
			}
			b[0] = 0 // slice is non-nil; set isNil flag to 0
			binary.BigEndian.PutUint32(b[1:], uint32(numElts))
			return nil
		}
		for i := 1; i < numElts+1; i++ { // Process each element in the slice
			subSize, subFunc, subErr := c.marshal(value.Index(i - 1))
			if subErr != nil {
				return 0, nil, subErr
			}
			size += subSize
			if i != numElts { // set offest for next function unless this is last ieration
				offsets[i+1] = offsets[i] + subSize
			}
			subFuncs[i] = subFunc
		}

		if subFuncsLen := len(subFuncs); subFuncsLen != len(offsets) {
			return 0, nil, fmt.Errorf("expected len(subFuncs) = %d. len(offsets) = %d. Should be same", subFuncsLen, len(offsets))
		}

		f = func(b []byte) error {
			bytesLen := len(b)
			for i, f := range subFuncs {
				offset := offsets[i]
				if offset > bytesLen {
					return fmt.Errorf("attempted out of bounds slice. offset: %d. bytesLen: %d", offset, bytesLen)
				}
				if err := f(b[offset:]); err != nil {
					return err
				}
			}
			return nil
		}
		return
	case reflect.Array:
		numElts := value.Len() // # elements in the slice/array (assumed to be <= 2^31 - 1)
		if numElts > math.MaxUint32 {
			return 0, nil, fmt.Errorf("array length, %d, exceeds maximum length, %d", numElts, math.MaxUint32)
		}

		size = 0
		// offsets[i] is the index in the byte array that subFuncs[i] will start writing at
		offsets := make([]int, numElts, numElts)
		offsets[1] = 4 // 4 for slice size
		subFuncs := make([]func([]byte) error, numElts, numElts)
		for i := 0; i < numElts; i++ { // Process each element in the array
			subSize, subFunc, subErr := c.marshal(value.Index(i))
			if subErr != nil {
				return 0, nil, subErr
			}
			size += subSize
			if i != numElts-1 { // set offest for next function unless this is last ieration
				offsets[i+1] = offsets[i] + subSize
			}
			subFuncs[i] = subFunc
		}

		if subFuncsLen := len(subFuncs); subFuncsLen != len(offsets) {
			return 0, nil, fmt.Errorf("expected len(subFuncs) = %d. len(offsets) = %d. Should be same", subFuncsLen, len(offsets))
		}

		f = func(b []byte) error {
			bytesLen := len(b)
			for i, f := range subFuncs {
				offset := offsets[i]
				if offset > bytesLen {
					return fmt.Errorf("attempted out of bounds slice")
				}
				if err := f(b[offset:]); err != nil {
					return err
				}
			}
			return nil
		}
		return
	case reflect.Struct:
		t := value.Type()
		numFields := t.NumField()
		size = 0
		// offsets[i] is the index in the byte array that subFuncs[i] will start writing at
		offsets := make([]int, 0, numFields)
		offsets = append(offsets, 0)
		subFuncs := make([]func([]byte) error, 0, numFields)
		for i := 0; i < numFields; i++ { // Go through all fields of this struct
			field := t.Field(i)
			if !shouldSerialize(field) { // Skip fields we don't need to serialize
				continue
			}
			if unicode.IsLower(rune(field.Name[0])) { // Can only marshal exported fields
				return 0, nil, fmt.Errorf("can't marshal unexported field %s", field.Name)
			}
			fieldVal := value.Field(i)                   // The field we're serializing
			subSize, subfunc, err := c.marshal(fieldVal) // Serialize the field
			if err != nil {
				return 0, nil, err
			}
			size += subSize
			subFuncs = append(subFuncs, subfunc)
			if i != numFields-1 { // set offset for next function if not last iteration
				offsets = append(offsets, offsets[len(offsets)-1]+subSize)
			}
		}

		f = func(b []byte) error {
			bytesLen := len(b)
			for i, f := range subFuncs {
				offset := offsets[i]
				if offset > bytesLen {
					return fmt.Errorf("attempted out of bounds slice")
				}
				if err := f(b[offset:]); err != nil {
					return err
				}
			}
			return nil
		}
		return
	default:
		return 0, nil, errUnknownType
	}
}

// Unmarshal unmarshals [bytes] into [dest], where
// [dest] must be a pointer or interface
func (c codec) Unmarshal(bytes []byte, dest interface{}) error {
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

	destVal := destPtr.Elem()
	bytesRead, err := c.unmarshal(bytes, destVal)
	if err != nil {
		return err
	}

	if l := len(bytes); l != bytesRead {
		return fmt.Errorf("%d leftover bytes after unmarshalling", l-bytesRead)
	}
	return nil
}

// Unmarshal bytes from [bytes] into [field]
// [field] must be addressable
// Returns the number of bytes read from [bytes]
func (c codec) unmarshal(bytes []byte, field reflect.Value) (int, error) {
	bytesLen := len(bytes)
	kind := field.Kind()
	switch kind {
	case reflect.Uint8:
		if bytesLen < 1 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
		}
		field.SetUint(uint64(bytes[0]))
		return 1, nil
	case reflect.Int8:
		if bytesLen < 1 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
		}
		field.SetInt(int64(bytes[0]))
		return 1, nil
	case reflect.Uint16:
		if bytesLen < 2 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 2 but is %d", bytesLen)
		}
		field.SetUint(uint64(binary.BigEndian.Uint16(bytes)))
		return 2, nil
	case reflect.Int16:
		if bytesLen < 2 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 2 but is %d", bytesLen)
		}
		field.SetInt(int64(binary.BigEndian.Uint16(bytes)))
		return 2, nil
	case reflect.Uint32:
		if bytesLen < 4 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 4 but is %d", bytesLen)
		}
		field.SetUint(uint64(binary.BigEndian.Uint32(bytes)))
		return 4, nil
	case reflect.Int32:
		if bytesLen < 4 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 4 but is %d", bytesLen)
		}
		field.SetInt(int64(binary.BigEndian.Uint32(bytes)))
		return 4, nil
	case reflect.Uint64:
		if bytesLen < 4 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 8 but is %d", bytesLen)
		}
		field.SetUint(uint64(binary.BigEndian.Uint64(bytes)))
		return 8, nil
	case reflect.Int64:
		if bytesLen < 4 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 8 but is %d", bytesLen)
		}
		field.SetInt(int64(binary.BigEndian.Uint64(bytes)))
		return 8, nil
	case reflect.Bool:
		if bytesLen < 1 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
		}
		if bytes[0] == 0 {
			field.SetBool(false)
		} else {
			field.SetBool(true)
		}
		return 1, nil
	case reflect.Slice:
		if bytesLen < 1 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 1 but is %d", bytesLen)
		}
		if bytes[0] == 1 { // isNil flag is 1 --> this slice is nil
			return 1, nil
		}

		numElts := int(binary.BigEndian.Uint32(bytes[1:])) // number of elements in the slice
		if numElts > c.maxSliceLen {
			return 0, fmt.Errorf("slice length, %d, exceeds maximum, %d", numElts, c.maxSliceLen)
		}

		// set [field] to be a slice of the appropriate type/capacity (right now [field] is nil)
		slice := reflect.MakeSlice(field.Type(), numElts, numElts)
		field.Set(slice)

		// Unmarshal each element into the appropriate index of the slice
		bytesRead := 5 // 1 for isNil flag, 4 for numElts
		for i := 0; i < numElts; i++ {
			if bytesRead > bytesLen {
				return 0, fmt.Errorf("attempted out of bounds slice")
			}
			n, err := c.unmarshal(bytes[bytesRead:], field.Index(i))
			if err != nil {
				return 0, err
			}
			bytesRead += n
		}
		return bytesRead, nil
	case reflect.Array:
		bytesRead := 0
		for i := 0; i < field.Len(); i++ {
			if bytesRead > bytesLen {
				return 0, fmt.Errorf("attempted out of bounds slice")
			}
			n, err := c.unmarshal(bytes[bytesRead:], field.Index(i))
			if err != nil {
				return 0, err
			}
			bytesRead += n
		}
		return bytesRead, nil
	case reflect.String:
		if bytesLen < 2 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 2 but is %d", bytesLen)
		}
		strLen := int(binary.BigEndian.Uint16(bytes))
		if bytesLen < 2+strLen {
			return 0, fmt.Errorf("expected len(bytes) to be at least %d but is %d", 2+strLen, bytesLen)
		}
		if strLen > 0 {
			field.SetString(string(bytes[2 : 2+strLen]))
		} else {
			field.SetString("")
		}
		return strLen + 2, nil
	case reflect.Interface:
		if bytesLen < 4 {
			return 0, fmt.Errorf("expected len(bytes) to be at least 4 but is %d", bytesLen)
		}

		// Get the type ID
		typeID := binary.BigEndian.Uint32(bytes)
		// Get a struct that implements the interface
		typ, ok := c.typeIDToType[typeID]
		if !ok {
			return 0, errUnmarshalUnregisteredType
		}
		// Ensure struct actually does implement the interface
		fieldType := field.Type()
		if !typ.Implements(fieldType) {
			return 0, fmt.Errorf("%s does not implement interface %s", typ, fieldType)
		}
		concreteInstancePtr := reflect.New(typ) // instance of the proper type
		// Unmarshal into the struct

		n, err := c.unmarshal(bytes[4:], concreteInstancePtr.Elem())
		if err != nil {
			return 0, err
		}
		// And assign the filled struct to the field
		field.Set(concreteInstancePtr.Elem())
		return n + 4, nil
	case reflect.Struct:
		// Type of this struct
		structType := reflect.TypeOf(field.Interface())
		// Go through all the fields and umarshal into each
		bytesRead := 0
		for i := 0; i < structType.NumField(); i++ {
			structField := structType.Field(i)
			if !shouldSerialize(structField) { // Skip fields we don't need to unmarshal
				continue
			}
			if unicode.IsLower(rune(structField.Name[0])) { // Only unmarshal into exported field
				return 0, errUnmarshalUnexportedField
			}
			field := field.Field(i) // Get the field
			if bytesRead > bytesLen {
				return 0, fmt.Errorf("attempted out of bounds slice")
			}
			n, err := c.unmarshal(bytes[bytesRead:], field) // Unmarshal into the field
			if err != nil {
				return 0, err
			}
			bytesRead += n
		}
		return bytesRead, nil
	case reflect.Ptr:
		// Get the type this pointer points to
		underlyingType := field.Type().Elem()
		// Create a new pointer to a new value of the underlying type
		underlyingValue := reflect.New(underlyingType)
		// Fill the value
		n, err := c.unmarshal(bytes, underlyingValue.Elem())
		if err != nil {
			return 0, err
		}
		// Assign to the top-level struct's member
		field.Set(underlyingValue)
		return n, nil
	case reflect.Invalid:
		return 0, errNil
	default:
		return 0, errUnknownType
	}
}

// Returns true iff [field] should be serialized
func shouldSerialize(field reflect.StructField) bool {
	return field.Tag.Get("serialize") == "true"
}
