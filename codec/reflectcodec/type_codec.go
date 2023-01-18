// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reflectcodec

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// DefaultTagName that enables serialization.
	DefaultTagName = "serialize"
)

var (
	ErrMaxMarshalSliceLimitExceeded = errors.New("maximum marshal slice limit exceeded")

	errMarshalNil   = errors.New("can't marshal nil pointer or interface")
	errUnmarshalNil = errors.New("can't unmarshal nil")
	errNeedPointer  = errors.New("argument to unmarshal must be a pointer")
	errExtraSpace   = errors.New("trailing buffer space")
)

var _ codec.Codec = (*genericCodec)(nil)

type TypeCodec interface {
	// UnpackPrefix unpacks the prefix of an interface from the given packer.
	// The prefix specifies the concrete type that the interface should be
	// deserialized into. This function returns a new instance of that concrete
	// type. The concrete type must implement the given type.
	UnpackPrefix(*wrappers.Packer, reflect.Type) (reflect.Value, error)

	// PackPrefix packs the prefix for the given type into the given packer.
	// This identifies the bytes that follow, which are the byte representation
	// of an interface, as having the given concrete type.
	// When deserializing the bytes, the prefix specifies which concrete type
	// to deserialize into.
	PackPrefix(*wrappers.Packer, reflect.Type) error

	// PrefixSize returns prefix length for the given type into the given
	// packer.
	PrefixSize(reflect.Type) int
}

// genericCodec handles marshaling and unmarshaling of structs with a generic
// implementation for interface encoding.
//
// A few notes:
// 1) We use "marshal" and "serialize" interchangeably, and "unmarshal" and "deserialize" interchangeably
// 2) To include a field of a struct in the serialized form, add the tag `{tagName}:"true"` to it. `{tagName}` defaults to `serialize`.
// 3) These typed members of a struct may be serialized:
//    bool, string, uint[8,16,32,64], int[8,16,32,64],
//	  structs, slices, arrays, interface.
//	  structs, slices and arrays can only be serialized if their constituent values can be.
// 4) To marshal an interface, you must pass a pointer to the value
// 5) To unmarshal an interface,  you must call codec.RegisterType([instance of the type that fulfills the interface]).
// 6) Serialized fields must be exported
// 7) nil slices are marshaled as empty slices
type genericCodec struct {
	typer       TypeCodec
	maxSliceLen uint32
	fielder     StructFielder
}

// New returns a new, concurrency-safe codec
func New(typer TypeCodec, tagNames []string, maxSliceLen uint32) codec.Codec {
	return &genericCodec{
		typer:       typer,
		maxSliceLen: maxSliceLen,
		fielder:     NewStructFielder(tagNames, maxSliceLen),
	}
}

func (c *genericCodec) Size(value interface{}) (int, error) {
	if value == nil {
		return 0, errMarshalNil // can't marshal nil
	}

	size, _, err := c.size(reflect.ValueOf(value))
	return size, err
}

// size returns the size of the value along with whether the value is constant sized.
func (c *genericCodec) size(value reflect.Value) (int, bool, error) {
	switch valueKind := value.Kind(); valueKind {
	case reflect.Uint8:
		return wrappers.ByteLen, true, nil
	case reflect.Int8:
		return wrappers.ByteLen, true, nil
	case reflect.Uint16:
		return wrappers.ShortLen, true, nil
	case reflect.Int16:
		return wrappers.ShortLen, true, nil
	case reflect.Uint32:
		return wrappers.IntLen, true, nil
	case reflect.Int32:
		return wrappers.IntLen, true, nil
	case reflect.Uint64:
		return wrappers.LongLen, true, nil
	case reflect.Int64:
		return wrappers.LongLen, true, nil
	case reflect.Bool:
		return wrappers.BoolLen, true, nil
	case reflect.String:
		return wrappers.StringLen(value.String()), false, nil
	case reflect.Ptr:
		if value.IsNil() {
			// Can't marshal nil pointers (but nil slices are fine)
			return 0, false, errMarshalNil
		}
		return c.size(value.Elem())

	case reflect.Interface:
		if value.IsNil() {
			// Can't marshal nil interfaces (but nil slices are fine)
			return 0, false, errMarshalNil
		}
		underlyingValue := value.Interface()
		underlyingType := reflect.TypeOf(underlyingValue)
		prefixSize := c.typer.PrefixSize(underlyingType)
		valueSize, _, err := c.size(value.Elem())
		if err != nil {
			return 0, false, err
		}
		return prefixSize + valueSize, false, nil

	case reflect.Slice:
		numElts := value.Len()
		if numElts == 0 {
			return wrappers.IntLen, false, nil
		}

		size, constSize, err := c.size(value.Index(0))
		if err != nil {
			return 0, false, err
		}

		// For fixed-size types we manually calculate lengths rather than
		// processing each element separately to improve performance.
		if constSize {
			return wrappers.IntLen + numElts*size, false, nil
		}

		for i := 1; i < numElts; i++ {
			innerSize, _, err := c.size(value.Index(i))
			if err != nil {
				return 0, false, err
			}
			size += innerSize
		}
		return wrappers.IntLen + size, false, nil

	case reflect.Array:
		numElts := value.Len()
		if numElts == 0 {
			return 0, true, nil
		}

		size, constSize, err := c.size(value.Index(0))
		if err != nil {
			return 0, false, err
		}

		// For fixed-size types we manually calculate lengths rather than
		// processing each element separately to improve performance.
		if constSize {
			return numElts * size, true, nil
		}

		for i := 1; i < numElts; i++ {
			innerSize, _, err := c.size(value.Index(i))
			if err != nil {
				return 0, false, err
			}
			size += innerSize
		}
		return size, false, nil

	case reflect.Struct:
		serializedFields, err := c.fielder.GetSerializedFields(value.Type())
		if err != nil {
			return 0, false, err
		}

		var (
			size      int
			constSize = true
		)
		for _, fieldDesc := range serializedFields {
			innerSize, innerConstSize, err := c.size(value.Field(fieldDesc.Index))
			if err != nil {
				return 0, false, err
			}
			size += innerSize
			constSize = constSize && innerConstSize
		}
		return size, constSize, nil

	default:
		return 0, false, fmt.Errorf("can't evaluate marshal length of unknown kind %s", valueKind)
	}
}

// To marshal an interface, [value] must be a pointer to the interface
func (c *genericCodec) MarshalInto(value interface{}, p *wrappers.Packer) error {
	if value == nil {
		return errMarshalNil // can't marshal nil
	}

	return c.marshal(reflect.ValueOf(value), p, c.maxSliceLen)
}

// marshal writes the byte representation of [value] to [p]
// [value]'s underlying value must not be a nil pointer or interface
// c.lock should be held for the duration of this function
func (c *genericCodec) marshal(value reflect.Value, p *wrappers.Packer, maxSliceLen uint32) error {
	switch valueKind := value.Kind(); valueKind {
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
	case reflect.Ptr:
		if value.IsNil() { // Can't marshal nil (except nil slices)
			return errMarshalNil
		}
		return c.marshal(value.Elem(), p, c.maxSliceLen)
	case reflect.Interface:
		if value.IsNil() { // Can't marshal nil (except nil slices)
			return errMarshalNil
		}
		underlyingValue := value.Interface()
		underlyingType := reflect.TypeOf(underlyingValue)
		if err := c.typer.PackPrefix(p, underlyingType); err != nil {
			return err
		}
		if err := c.marshal(value.Elem(), p, c.maxSliceLen); err != nil {
			return err
		}
		return p.Err
	case reflect.Slice:
		numElts := value.Len() // # elements in the slice/array. 0 if this slice is nil.
		if uint32(numElts) > maxSliceLen {
			return fmt.Errorf("%w; slice length, %d, exceeds maximum length, %d",
				ErrMaxMarshalSliceLimitExceeded,
				numElts,
				maxSliceLen)
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
			if err := c.marshal(value.Index(i), p, c.maxSliceLen); err != nil {
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
		if uint32(numElts) > c.maxSliceLen {
			return fmt.Errorf("%w; array length, %d, exceeds maximum length, %d", ErrMaxMarshalSliceLimitExceeded, numElts, c.maxSliceLen)
		}
		for i := 0; i < numElts; i++ { // Process each element in the array
			if err := c.marshal(value.Index(i), p, c.maxSliceLen); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		serializedFields, err := c.fielder.GetSerializedFields(value.Type())
		if err != nil {
			return err
		}
		for _, fieldDesc := range serializedFields { // Go through all fields of this struct that are serialized
			if err := c.marshal(value.Field(fieldDesc.Index), p, fieldDesc.MaxSliceLen); err != nil { // Serialize the field and write to byte array
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
func (c *genericCodec) Unmarshal(bytes []byte, dest interface{}) error {
	if dest == nil {
		return errUnmarshalNil
	}

	p := wrappers.Packer{
		Bytes: bytes,
	}
	destPtr := reflect.ValueOf(dest)
	if destPtr.Kind() != reflect.Ptr {
		return errNeedPointer
	}
	if err := c.unmarshal(&p, destPtr.Elem(), c.maxSliceLen); err != nil {
		return err
	}
	if p.Offset != len(bytes) {
		return errExtraSpace
	}
	return nil
}

// Unmarshal from p.Bytes into [value]. [value] must be addressable.
// c.lock should be held for the duration of this function
func (c *genericCodec) unmarshal(p *wrappers.Packer, value reflect.Value, maxSliceLen uint32) error {
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
		numElts32 := p.UnpackInt()
		if p.Err != nil {
			return fmt.Errorf("couldn't unmarshal slice: %w", p.Err)
		}
		if numElts32 > maxSliceLen {
			return fmt.Errorf("%w; array length, %d, exceeds maximum length, %d",
				ErrMaxMarshalSliceLimitExceeded,
				numElts32,
				maxSliceLen)
		}
		if numElts32 > math.MaxInt32 {
			return fmt.Errorf("%w; array length, %d, exceeds maximum length, %d",
				ErrMaxMarshalSliceLimitExceeded,
				numElts32,
				math.MaxInt32)
		}
		numElts := int(numElts32)

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
			if err := c.unmarshal(p, value.Index(i), c.maxSliceLen); err != nil {
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
			if err := c.unmarshal(p, value.Index(i), c.maxSliceLen); err != nil {
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
		intfImplementor, err := c.typer.UnpackPrefix(p, value.Type())
		if err != nil {
			return err
		}
		// Unmarshal into the struct
		if err := c.unmarshal(p, intfImplementor, c.maxSliceLen); err != nil {
			return fmt.Errorf("couldn't unmarshal interface: %w", err)
		}
		// And assign the filled struct to the value
		value.Set(intfImplementor)
		return nil
	case reflect.Struct:
		// Get indices of fields that will be unmarshaled into
		serializedFieldIndices, err := c.fielder.GetSerializedFields(value.Type())
		if err != nil {
			return fmt.Errorf("couldn't unmarshal struct: %w", err)
		}
		// Go through the fields and umarshal into them
		for _, fieldDesc := range serializedFieldIndices {
			if err := c.unmarshal(p, value.Field(fieldDesc.Index), fieldDesc.MaxSliceLen); err != nil {
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
		if err := c.unmarshal(p, v.Elem(), c.maxSliceLen); err != nil {
			return fmt.Errorf("couldn't unmarshal pointer: %w", err)
		}
		// Assign to the top-level struct's member
		value.Set(v)
		return nil
	default:
		return fmt.Errorf("can't unmarshal unknown type %s", value.Kind().String())
	}
}
