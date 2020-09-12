// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"unicode"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultMaxSize        = 1 << 18 // default max size, in bytes, of something being marshalled by Marshal()
	defaultMaxSliceLength = 1 << 18 // default max length of a slice being marshalled by Marshal(). Should be <= math.MaxUint32.
	// initial capacity of byte slice that values are marshaled into.
	// Larger value --> need less memory allocations but possibly have allocated but unused memory
	// Smaller value --> need more memory allocations but more efficient use of allocated memory
	initialSliceCap = 128
	// The current version of the codec. Right now there is only one codec version, but in the future the
	// codec may change. All serialized blobs begin with the codec version.
	version = uint16(0)
)

var (
	errMarshalNil        = errors.New("can't marshal nil pointer or interface")
	errUnmarshalNil      = errors.New("can't unmarshal nil")
	errNeedPointer       = errors.New("argument to unmarshal must be a pointer")
	errCantPackVersion   = errors.New("couldn't pack codec version")
	errCantUnpackVersion = errors.New("couldn't unpack codec version")
)

// Codec handles marshaling and unmarshaling of structs
type codec struct {
	lock        sync.Mutex
	version     uint16
	maxSize     int
	maxSliceLen int

	nextTypeID   uint32
	typeIDToType map[uint32]reflect.Type
	typeToTypeID map[reflect.Type]uint32

	// Key: a struct type
	// Value: Slice where each element is index in the struct type
	// of a field that is serialized/deserialized
	// e.g. Foo --> [1,5,8] means Foo.Field(1), etc. are to be serialized/deserialized
	// We assume this cache is pretty small (a few hundred keys at most)
	// and doesn't take up much memory
	serializedFieldIndices map[reflect.Type][]int
}

// Codec marshals and unmarshals
type Codec interface {
	Skip(int)
	RegisterType(interface{}) error
	Marshal(interface{}) ([]byte, error)
	Unmarshal([]byte, interface{}) error
}

// New returns a new, concurrency-safe codec
func New(maxSize, maxSliceLen int) Codec {
	return &codec{
		maxSize:                maxSize,
		maxSliceLen:            maxSliceLen,
		version:                version,
		nextTypeID:             0,
		typeIDToType:           map[uint32]reflect.Type{},
		typeToTypeID:           map[reflect.Type]uint32{},
		serializedFieldIndices: map[reflect.Type][]int{},
	}
}

// NewDefault returns a new codec with reasonable default values
func NewDefault() Codec { return New(defaultMaxSize, defaultMaxSliceLength) }

// Skip some number of type IDs
func (c *codec) Skip(num int) {
	c.lock.Lock()
	c.nextTypeID += uint32(num)
	c.lock.Unlock()
}

// RegisterType is used to register types that may be unmarshaled into an interface
// [val] is a value of the type being registered
func (c *codec) RegisterType(val interface{}) error {
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
	c.lock.Lock()
	defer c.lock.Unlock()
	if value == nil {
		return nil, errMarshalNil // can't marshal nil
	}
	p := &wrappers.Packer{MaxSize: c.maxSize, Bytes: make([]byte, 0, initialSliceCap)}
	if p.PackShort(c.version); p.Errored() {
		return nil, errCantPackVersion // Should never happen
	} else if err := c.marshal(reflect.ValueOf(value), p); err != nil {
		return nil, err
	}
	return p.Bytes, nil
}

// marshal writes the byte representation of [value] to [p]
// [value]'s underlying value must not be a nil pointer or interface
// c.lock should be held for the duration of this function
func (c *codec) marshal(value reflect.Value, p *wrappers.Packer) error {
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
func (c *codec) Unmarshal(bytes []byte, dest interface{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	switch {
	case len(bytes) > c.maxSize:
		return fmt.Errorf("byte array exceeds maximum length, %d", c.maxSize)
	case dest == nil:
		return errUnmarshalNil
	}
	p := &wrappers.Packer{MaxSize: c.maxSize, Bytes: bytes}
	if destPtr := reflect.ValueOf(dest); destPtr.Kind() != reflect.Ptr {
		return errNeedPointer
	} else if codecVersion := p.UnpackShort(); p.Errored() { // Make sure the codec version is correct
		return errCantUnpackVersion
	} else if codecVersion != c.version {
		return fmt.Errorf("expected codec version to be %d but is %d", c.version, codecVersion)
	} else if err := c.unmarshal(p, destPtr.Elem()); err != nil {
		return err
	}
	return nil
}

// Unmarshal from p.Bytes into [value]. [value] must be addressable.
// c.lock should be held for the duration of this function
func (c *codec) unmarshal(p *wrappers.Packer, value reflect.Value) error {
	switch value.Kind() {
	case reflect.Uint8:
		value.SetUint(uint64(p.UnpackByte()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint8: %s", p.Err)
		}
		return nil
	case reflect.Int8:
		value.SetInt(int64(p.UnpackByte()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int8: %s", p.Err)
		}
		return nil
	case reflect.Uint16:
		value.SetUint(uint64(p.UnpackShort()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint16: %s", p.Err)
		}
		return nil
	case reflect.Int16:
		value.SetInt(int64(p.UnpackShort()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int16: %s", p.Err)
		}
		return nil
	case reflect.Uint32:
		value.SetUint(uint64(p.UnpackInt()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint32: %s", p.Err)
		}
		return nil
	case reflect.Int32:
		value.SetInt(int64(p.UnpackInt()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int32: %s", p.Err)
		}
		return nil
	case reflect.Uint64:
		value.SetUint(uint64(p.UnpackLong()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal uint64: %s", p.Err)
		}
		return nil
	case reflect.Int64:
		value.SetInt(int64(p.UnpackLong()))
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal int64: %s", p.Err)
		}
		return nil
	case reflect.Bool:
		value.SetBool(p.UnpackBool())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal bool: %s", p.Err)
		}
		return nil
	case reflect.Slice:
		numElts := int(p.UnpackInt())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal slice: %s", p.Err)
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
				return fmt.Errorf("couldn't unmarshal slice element: %s", err)
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
				return fmt.Errorf("couldn't unmarshal array element: %s", err)
			}
		}
		return nil
	case reflect.String:
		value.SetString(p.UnpackStr())
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal string: %s", p.Err)
		}
		return nil
	case reflect.Interface:
		typeID := p.UnpackInt() // Get the type ID
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal interface: %s", p.Err)
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
			return fmt.Errorf("couldn't unmarshal interface: %s", err)
		}
		// And assign the filled struct to the value
		value.Set(intfImplementor)
		return nil
	case reflect.Struct:
		// Get indices of fields that will be unmarshaled into
		serializedFieldIndices, err := c.getSerializedFieldIndices(value.Type())
		if err != nil {
			return fmt.Errorf("couldn't unmarshal struct: %s", err)
		}
		// Go through the fields and umarshal into them
		for _, index := range serializedFieldIndices {
			if err := c.unmarshal(p, value.Field(index)); err != nil {
				return fmt.Errorf("couldn't unmarshal struct: %s", err)
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
			return fmt.Errorf("couldn't unmarshal pointer: %s", err)
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
func (c *codec) getSerializedFieldIndices(t reflect.Type) ([]int, error) {
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
		if field.Tag.Get("serialize") != "true" { // Skip fields we don't need to serialize
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
