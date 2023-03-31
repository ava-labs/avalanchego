// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var Tests = []func(c GeneralCodec, t testing.TB){
	TestStruct,
	TestRegisterStructTwice,
	TestUInt32,
	TestUIntPtr,
	TestSlice,
	TestMaxSizeSlice,
	TestBool,
	TestArray,
	TestBigArray,
	TestPointerToStruct,
	TestSliceOfStruct,
	TestInterface,
	TestSliceOfInterface,
	TestArrayOfInterface,
	TestPointerToInterface,
	TestString,
	TestNilSlice,
	TestSerializeUnexportedField,
	TestSerializeOfNoSerializeField,
	TestNilSliceSerialization,
	TestEmptySliceSerialization,
	TestSliceWithEmptySerialization,
	TestSliceWithEmptySerializationOutOfMemory,
	TestSliceTooLarge,
	TestNegativeNumbers,
	TestTooLargeUnmarshal,
	TestUnmarshalInvalidInterface,
	TestRestrictedSlice,
	TestExtraSpace,
	TestSliceLengthOverflow,
}

var MultipleTagsTests = []func(c GeneralCodec, t testing.TB){
	TestMultipleTags,
}

// The below structs and interfaces exist
// for the sake of testing

var (
	_ Foo = (*MyInnerStruct)(nil)
	_ Foo = (*MyInnerStruct2)(nil)
)

type Foo interface {
	Foo() int
}

type MyInnerStruct struct {
	Str string `serialize:"true"`
}

func (*MyInnerStruct) Foo() int {
	return 1
}

type MyInnerStruct2 struct {
	Bool bool `serialize:"true"`
}

func (*MyInnerStruct2) Foo() int {
	return 2
}

// MyInnerStruct3 embeds Foo, an interface,
// so it has to implement TypeID and ConcreteInstance
type MyInnerStruct3 struct {
	Str string        `serialize:"true"`
	M1  MyInnerStruct `serialize:"true"`
	F   Foo           `serialize:"true"`
}

type myStruct struct {
	InnerStruct  MyInnerStruct      `serialize:"true"`
	InnerStruct2 *MyInnerStruct     `serialize:"true"`
	Member1      int64              `serialize:"true"`
	Member2      uint16             `serialize:"true"`
	MyArray2     [5]string          `serialize:"true"`
	MyArray3     [3]MyInnerStruct   `serialize:"true"`
	MyArray4     [2]*MyInnerStruct2 `serialize:"true"`
	MySlice      []byte             `serialize:"true"`
	MySlice2     []string           `serialize:"true"`
	MySlice3     []MyInnerStruct    `serialize:"true"`
	MySlice4     []*MyInnerStruct2  `serialize:"true"`
	MyArray      [4]byte            `serialize:"true"`
	MyInterface  Foo                `serialize:"true"`
	MySlice5     []Foo              `serialize:"true"`
	InnerStruct3 MyInnerStruct3     `serialize:"true"`
	MyPointer    *Foo               `serialize:"true"`
}

// Test marshaling/unmarshaling a complicated struct
func TestStruct(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	temp := Foo(&MyInnerStruct{})
	myStructInstance := myStruct{
		InnerStruct:  MyInnerStruct{"hello"},
		InnerStruct2: &MyInnerStruct{"yello"},
		Member1:      1,
		Member2:      2,
		MySlice:      []byte{1, 2, 3, 4},
		MySlice2:     []string{"one", "two", "three"},
		MySlice3:     []MyInnerStruct{{"abc"}, {"ab"}, {"c"}},
		MySlice4:     []*MyInnerStruct2{{true}, {}},
		MySlice5:     []Foo{&MyInnerStruct2{true}, &MyInnerStruct2{}},
		MyArray:      [4]byte{5, 6, 7, 8},
		MyArray2:     [5]string{"four", "five", "six", "seven"},
		MyArray3:     [3]MyInnerStruct{{"d"}, {"e"}, {"f"}},
		MyArray4:     [2]*MyInnerStruct2{{}, {true}},
		MyInterface:  &MyInnerStruct{"yeet"},
		InnerStruct3: MyInnerStruct3{
			Str: "str",
			M1: MyInnerStruct{
				Str: "other str",
			},
			F: &MyInnerStruct2{},
		},
		MyPointer: &temp,
	}

	manager := NewDefaultManager()
	// Register the types that may be unmarshaled into interfaces
	require.NoError(codec.RegisterType(&MyInnerStruct{}))
	require.NoError(codec.RegisterType(&MyInnerStruct2{}))
	require.NoError(manager.RegisterCodec(0, codec))

	myStructBytes, err := manager.Marshal(0, myStructInstance)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myStructInstance)
	require.NoError(err)
	require.Equal(len(myStructBytes), bytesLen)

	myStructUnmarshaled := &myStruct{}
	version, err := manager.Unmarshal(myStructBytes, myStructUnmarshaled)
	require.NoError(err)

	require.Equal(uint16(0), version)
	require.Equal(myStructInstance, *myStructUnmarshaled)
}

func TestRegisterStructTwice(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	require.NoError(codec.RegisterType(&MyInnerStruct{}))
	require.Error(codec.RegisterType(&MyInnerStruct{}))
}

func TestUInt32(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	number := uint32(500)

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, number)
	require.NoError(err)

	bytesLen, err := manager.Size(0, number)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var numberUnmarshaled uint32
	version, err := manager.Unmarshal(bytes, &numberUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(number, numberUnmarshaled)
}

func TestUIntPtr(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := NewDefaultManager()

	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	number := uintptr(500)
	_, err = manager.Marshal(0, number)
	require.Error(err)
}

func TestSlice(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := []bool{true, false, true, true}
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var sliceUnmarshaled []bool
	version, err := manager.Unmarshal(bytes, &sliceUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(mySlice, sliceUnmarshaled)
}

// Test marshalling/unmarshalling largest possible slice
func TestMaxSizeSlice(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := make([]string, math.MaxUint16)
	mySlice[0] = "first!"
	mySlice[math.MaxUint16-1] = "last!"
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var sliceUnmarshaled []string
	version, err := manager.Unmarshal(bytes, &sliceUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(mySlice, sliceUnmarshaled)
}

// Test marshalling a bool
func TestBool(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	myBool := true
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myBool)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myBool)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var boolUnmarshaled bool
	version, err := manager.Unmarshal(bytes, &boolUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myBool, boolUnmarshaled)
}

// Test marshalling an array
func TestArray(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	myArr := [5]uint64{5, 6, 7, 8, 9}
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myArr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myArr)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var myArrUnmarshaled [5]uint64
	version, err := manager.Unmarshal(bytes, &myArrUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myArr, myArrUnmarshaled)
}

// Test marshalling a really big array
func TestBigArray(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	myArr := [30000]uint64{5, 6, 7, 8, 9}
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myArr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myArr)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var myArrUnmarshaled [30000]uint64
	version, err := manager.Unmarshal(bytes, &myArrUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myArr, myArrUnmarshaled)
}

// Test marshalling a pointer to a struct
func TestPointerToStruct(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	myPtr := &MyInnerStruct{Str: "Hello!"}
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myPtr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myPtr)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var myPtrUnmarshaled *MyInnerStruct
	version, err := manager.Unmarshal(bytes, &myPtrUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myPtr, myPtrUnmarshaled)
}

// Test marshalling a slice of structs
func TestSliceOfStruct(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := []MyInnerStruct3{
		{
			Str: "One",
			M1:  MyInnerStruct{"Two"},
			F:   &MyInnerStruct{"Three"},
		},
		{
			Str: "Four",
			M1:  MyInnerStruct{"Five"},
			F:   &MyInnerStruct{"Six"},
		},
	}
	err := codec.RegisterType(&MyInnerStruct{})
	require.NoError(err)

	manager := NewDefaultManager()
	err = manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var mySliceUnmarshaled []MyInnerStruct3
	version, err := manager.Unmarshal(bytes, &mySliceUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(mySlice, mySliceUnmarshaled)
}

// Test marshalling an interface
func TestInterface(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	err := codec.RegisterType(&MyInnerStruct2{})
	require.NoError(err)

	manager := NewDefaultManager()
	err = manager.RegisterCodec(0, codec)
	require.NoError(err)

	var f Foo = &MyInnerStruct2{true}
	bytes, err := manager.Marshal(0, &f)
	require.NoError(err)

	bytesLen, err := manager.Size(0, &f)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var unmarshaledFoo Foo
	version, err := manager.Unmarshal(bytes, &unmarshaledFoo)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(f, unmarshaledFoo)
}

// Test marshalling a slice of interfaces
func TestSliceOfInterface(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := []Foo{
		&MyInnerStruct{
			Str: "Hello",
		},
		&MyInnerStruct{
			Str: ", World!",
		},
	}
	err := codec.RegisterType(&MyInnerStruct{})
	require.NoError(err)

	manager := NewDefaultManager()
	err = manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var mySliceUnmarshaled []Foo
	version, err := manager.Unmarshal(bytes, &mySliceUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(mySlice, mySliceUnmarshaled)
}

// Test marshalling an array of interfaces
func TestArrayOfInterface(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	myArray := [2]Foo{
		&MyInnerStruct{
			Str: "Hello",
		},
		&MyInnerStruct{
			Str: ", World!",
		},
	}
	err := codec.RegisterType(&MyInnerStruct{})
	require.NoError(err)

	manager := NewDefaultManager()
	err = manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myArray)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myArray)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var myArrayUnmarshaled [2]Foo
	version, err := manager.Unmarshal(bytes, &myArrayUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myArray, myArrayUnmarshaled)
}

// Test marshalling a pointer to an interface
func TestPointerToInterface(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	var myinnerStruct Foo = &MyInnerStruct{Str: "Hello!"}
	myPtr := &myinnerStruct

	err := codec.RegisterType(&MyInnerStruct{})
	require.NoError(err)

	manager := NewDefaultManager()
	err = manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, &myPtr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, &myPtr)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var myPtrUnmarshaled *Foo
	version, err := manager.Unmarshal(bytes, &myPtrUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myPtr, myPtrUnmarshaled)
}

// Test marshalling a string
func TestString(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	myString := "Ayy"
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myString)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myString)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var stringUnmarshaled string
	version, err := manager.Unmarshal(bytes, &stringUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myString, stringUnmarshaled)
}

// Ensure a nil slice is unmarshaled to slice with length 0
func TestNilSlice(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type structWithSlice struct {
		Slice []byte `serialize:"true"`
	}

	myStruct := structWithSlice{Slice: nil}
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, myStruct)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myStruct)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	var structUnmarshaled structWithSlice
	version, err := manager.Unmarshal(bytes, &structUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(0, len(structUnmarshaled.Slice))
}

// Ensure that trying to serialize a struct with an unexported member
// that has `serialize:"true"` returns error
func TestSerializeUnexportedField(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type s struct {
		ExportedField   string `serialize:"true"`
		unexportedField string `serialize:"true"` //nolint:revive
	}

	myS := s{
		ExportedField:   "Hello, ",
		unexportedField: "world!",
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	_, err = manager.Marshal(0, myS)
	require.Error(err)

	_, err = manager.Size(0, myS)
	require.Error(err)
}

func TestSerializeOfNoSerializeField(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type s struct {
		SerializedField   string `serialize:"true"`
		UnserializedField string `serialize:"false"`
		UnmarkedField     string
	}
	myS := s{
		SerializedField:   "Serialize me",
		UnserializedField: "Do not serialize me",
		UnmarkedField:     "No declared serialize",
	}
	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	marshalled, err := manager.Marshal(0, myS)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myS)
	require.NoError(err)
	require.Equal(len(marshalled), bytesLen)

	unmarshalled := s{}
	version, err := manager.Unmarshal(marshalled, &unmarshalled)
	require.NoError(err)
	require.Equal(uint16(0), version)

	expectedUnmarshalled := s{SerializedField: "Serialize me"}
	require.Equal(expectedUnmarshalled, unmarshalled)
}

// Test marshalling of nil slice
func TestNilSliceSerialization(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type simpleSliceStruct struct {
		Arr []uint32 `serialize:"true"`
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	val := &simpleSliceStruct{}
	expected := []byte{0, 0, 0, 0, 0, 0} // 0 for codec version, then nil slice marshaled as 0 length slice
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Equal(len(result), bytesLen)

	valUnmarshaled := &simpleSliceStruct{}
	version, err := manager.Unmarshal(result, &valUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(0, len(valUnmarshaled.Arr))
}

// Test marshaling a slice that has 0 elements (but isn't nil)
func TestEmptySliceSerialization(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type simpleSliceStruct struct {
		Arr []uint32 `serialize:"true"`
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	val := &simpleSliceStruct{Arr: make([]uint32, 0, 1)}
	expected := []byte{0, 0, 0, 0, 0, 0} // 0 for codec version (uint16) and 0 for size (uint32)
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Equal(len(result), bytesLen)

	valUnmarshaled := &simpleSliceStruct{}
	version, err := manager.Unmarshal(result, &valUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(val, valUnmarshaled)
}

// Test marshaling slice that is not nil and not empty
func TestSliceWithEmptySerialization(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type emptyStruct struct{}

	type nestedSliceStruct struct {
		Arr []emptyStruct `serialize:"true"`
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	val := &nestedSliceStruct{
		Arr: make([]emptyStruct, 1000),
	}
	expected := []byte{0x00, 0x00, 0x00, 0x00, 0x03, 0xE8} // codec version (0x00, 0x00) then 1000 for numElts
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Equal(len(result), bytesLen)

	unmarshaled := nestedSliceStruct{}
	version, err := manager.Unmarshal(expected, &unmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(1000, len(unmarshaled.Arr))
}

func TestSliceWithEmptySerializationOutOfMemory(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type emptyStruct struct{}

	type nestedSliceStruct struct {
		Arr []emptyStruct `serialize:"true"`
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	val := &nestedSliceStruct{
		Arr: make([]emptyStruct, math.MaxInt32),
	}
	_, err = manager.Marshal(0, val)
	require.Error(err)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Equal(6, bytesLen) // 2 byte codec version + 4 byte length prefix
}

func TestSliceTooLarge(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	val := []struct{}{}
	b := []byte{0x00, 0x00, 0xff, 0xff, 0xff, 0xff}
	_, err = manager.Unmarshal(b, &val)
	require.Error(err)
}

// Ensure serializing structs with negative number members works
func TestNegativeNumbers(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type s struct {
		MyInt8  int8  `serialize:"true"`
		MyInt16 int16 `serialize:"true"`
		MyInt32 int32 `serialize:"true"`
		MyInt64 int64 `serialize:"true"`
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	myS := s{-1, -2, -3, -4}
	bytes, err := manager.Marshal(0, myS)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myS)
	require.NoError(err)
	require.Equal(len(bytes), bytesLen)

	mySUnmarshaled := s{}
	version, err := manager.Unmarshal(bytes, &mySUnmarshaled)
	require.NoError(err)
	require.Equal(uint16(0), version)
	require.Equal(myS, mySUnmarshaled)
}

// Ensure deserializing structs with too many bytes errors correctly
func TestTooLargeUnmarshal(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type inner struct {
		B uint16 `serialize:"true"`
	}
	bytes := []byte{0, 0, 0, 0}

	manager := NewManager(3)
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	s := inner{}
	_, err = manager.Unmarshal(bytes, &s)
	require.Error(err)
}

type outerInterface interface {
	ToInt() int
}

type outer struct {
	Interface outerInterface `serialize:"true"`
}

type innerInterface struct{}

func (*innerInterface) ToInt() int {
	return 0
}

type innerNoInterface struct{}

// Ensure deserializing structs into the wrong interface errors gracefully
func TestUnmarshalInvalidInterface(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := NewDefaultManager()
	require.NoError(codec.RegisterType(&innerInterface{}))
	require.NoError(codec.RegisterType(&innerNoInterface{}))
	require.NoError(manager.RegisterCodec(0, codec))

	{
		bytes := []byte{0, 0, 0, 0, 0, 0}
		s := outer{}
		version, err := manager.Unmarshal(bytes, &s)
		require.NoError(err)
		require.Equal(uint16(0), version)
	}
	{
		bytes := []byte{0, 0, 0, 0, 0, 1}
		s := outer{}
		_, err := manager.Unmarshal(bytes, &s)
		require.Error(err)
	}
}

// Ensure deserializing slices that have been length restricted errors correctly
func TestRestrictedSlice(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type inner struct {
		Bytes []byte `serialize:"true" len:"2"`
	}
	bytes := []byte{0, 0, 0, 0, 0, 3, 0, 1, 2}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	s := inner{}
	_, err = manager.Unmarshal(bytes, &s)
	require.Error(err)

	s.Bytes = []byte{0, 1, 2}
	_, err = manager.Marshal(0, s)
	require.Error(err)
}

// Test unmarshaling something with extra data
func TestExtraSpace(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	// codec version 0x0000 then 0x01 for b then 0x02 as extra data.
	byteSlice := []byte{0x00, 0x00, 0x01, 0x02}
	var b byte
	_, err = manager.Unmarshal(byteSlice, &b)
	require.Error(err)
}

// Ensure deserializing slices that have been length restricted errors correctly
func TestSliceLengthOverflow(codec GeneralCodec, t testing.TB) {
	require := require.New(t)

	type inner struct {
		Vals []uint32 `serialize:"true" len:"2"`
	}
	bytes := []byte{
		// Codec Version:
		0x00, 0x00,
		// Slice Length:
		0xff, 0xff, 0xff, 0xff,
	}

	manager := NewDefaultManager()
	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	s := inner{}
	_, err = manager.Unmarshal(bytes, &s)
	require.Error(err)
}

type MultipleVersionsStruct struct {
	BothTags    string `tag1:"true" tag2:"true"`
	SingleTag1  string `tag1:"true"`
	SingleTag2  string `tag2:"true"`
	EitherTags1 string `tag1:"false" tag2:"true"`
	EitherTags2 string `tag1:"true" tag2:"false"`
	NoTags      string `tag1:"false" tag2:"false"`
}

func TestMultipleTags(codec GeneralCodec, t testing.TB) {
	// received codec is expected to have both v1 and v2 registered as tags
	inputs := MultipleVersionsStruct{
		BothTags:    "both Tags",
		SingleTag1:  "Only Tag1",
		SingleTag2:  "Only Tag2",
		EitherTags1: "Tag2 is false",
		EitherTags2: "Tag1 is false",
		NoTags:      "Neither Tag",
	}

	manager := NewDefaultManager()
	for _, codecVersion := range []uint16{0, 1, 2022} {
		require := require.New(t)

		err := manager.RegisterCodec(codecVersion, codec)
		require.NoError(err)

		bytes, err := manager.Marshal(codecVersion, inputs)
		require.NoError(err)

		output := MultipleVersionsStruct{}
		_, err = manager.Unmarshal(bytes, &output)
		require.NoError(err)

		require.Equal(inputs.BothTags, output.BothTags)
		require.Equal(inputs.SingleTag1, output.SingleTag1)
		require.Equal(inputs.SingleTag2, output.SingleTag2)
		require.Equal(inputs.EitherTags1, output.EitherTags1)
		require.Equal(inputs.EitherTags2, output.EitherTags2)
		require.Empty(output.NoTags)
	}
}
