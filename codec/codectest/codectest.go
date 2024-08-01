// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codectest

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
)

var (
	Tests = []func(c codec.GeneralCodec, t testing.TB){
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
		TestSliceWithEmptySerializationError,
		TestMapWithEmptySerialization,
		TestMapWithEmptySerializationError,
		TestSliceTooLarge,
		TestNegativeNumbers,
		TestTooLargeUnmarshal,
		TestUnmarshalInvalidInterface,
		TestExtraSpace,
		TestSliceLengthOverflow,
		TestMap,
		TestCanMarshalLargeSlices,
	}

	MultipleTagsTests = []func(c codec.GeneralCodec, t testing.TB){
		TestMultipleTags,
	}
)

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
	InnerStruct  MyInnerStruct               `serialize:"true"`
	InnerStruct2 *MyInnerStruct              `serialize:"true"`
	Member1      int64                       `serialize:"true"`
	Member2      uint16                      `serialize:"true"`
	MyArray2     [5]string                   `serialize:"true"`
	MyArray3     [3]MyInnerStruct            `serialize:"true"`
	MyArray4     [2]*MyInnerStruct2          `serialize:"true"`
	MySlice      []byte                      `serialize:"true"`
	MySlice2     []string                    `serialize:"true"`
	MySlice3     []MyInnerStruct             `serialize:"true"`
	MySlice4     []*MyInnerStruct2           `serialize:"true"`
	MyArray      [4]byte                     `serialize:"true"`
	MyInterface  Foo                         `serialize:"true"`
	MySlice5     []Foo                       `serialize:"true"`
	InnerStruct3 MyInnerStruct3              `serialize:"true"`
	MyPointer    *Foo                        `serialize:"true"`
	MyMap1       map[string]string           `serialize:"true"`
	MyMap2       map[int32][]MyInnerStruct3  `serialize:"true"`
	MyMap3       map[MyInnerStruct2][]int32  `serialize:"true"`
	MyMap4       map[int32]*int32            `serialize:"true"`
	MyMap5       map[int32]int32             `serialize:"true"`
	MyMap6       map[[5]int32]int32          `serialize:"true"`
	MyMap7       map[interface{}]interface{} `serialize:"true"`
	Uint8        uint8                       `serialize:"true"`
	Int8         int8                        `serialize:"true"`
	Uint16       uint16                      `serialize:"true"`
	Int16        int16                       `serialize:"true"`
	Uint32       uint32                      `serialize:"true"`
	Int32        int32                       `serialize:"true"`
	Uint64       uint64                      `serialize:"true"`
	Int64        int64                       `serialize:"true"`
	Bool         bool                        `serialize:"true"`
	String       string                      `serialize:"true"`
}

// Test marshaling/unmarshaling a complicated struct
func TestStruct(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	temp := Foo(&MyInnerStruct{})
	myMap3 := make(map[MyInnerStruct2][]int32)
	myMap3[MyInnerStruct2{false}] = []int32{991, 12}
	myMap3[MyInnerStruct2{true}] = []int32{1911, 1921}

	myMap4 := make(map[int32]*int32)
	zero := int32(0)
	one := int32(1)
	myMap4[0] = &zero
	myMap4[1] = &one

	myMap6 := make(map[[5]int32]int32)
	myMap6[[5]int32{0, 1, 2, 3, 4}] = 1
	myMap6[[5]int32{1, 2, 3, 4, 5}] = 2

	myMap7 := make(map[interface{}]interface{})
	myMap7["key"] = "value"
	myMap7[int32(1)] = int32(2)

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
		MyMap1: map[string]string{
			"test": "test",
		},
		MyMap2: map[int32][]MyInnerStruct3{
			199921: {
				{
					Str: "str-1",
					M1: MyInnerStruct{
						Str: "other str",
					},
					F: &MyInnerStruct2{},
				},
				{
					Str: "str-2",
					M1: MyInnerStruct{
						Str: "other str",
					},
					F: &MyInnerStruct2{},
				},
			},
			1921: {
				{
					Str: "str0",
					M1: MyInnerStruct{
						Str: "other str",
					},
					F: &MyInnerStruct2{},
				},
				{
					Str: "str1",
					M1: MyInnerStruct{
						Str: "other str",
					},
					F: &MyInnerStruct2{},
				},
			},
		},
		MyMap3: myMap3,
		MyMap4: myMap4,
		MyMap6: myMap6,
		MyMap7: myMap7,
	}

	manager := codec.NewDefaultManager()
	// Register the types that may be unmarshaled into interfaces
	require.NoError(cdec.RegisterType(&MyInnerStruct{}))
	require.NoError(cdec.RegisterType(&MyInnerStruct2{}))
	require.NoError(cdec.RegisterType(""))
	require.NoError(cdec.RegisterType(int32(0)))
	require.NoError(manager.RegisterCodec(0, cdec))

	myStructBytes, err := manager.Marshal(0, myStructInstance)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myStructInstance)
	require.NoError(err)
	require.Len(myStructBytes, bytesLen)

	myStructUnmarshaled := &myStruct{}
	version, err := manager.Unmarshal(myStructBytes, myStructUnmarshaled)
	require.NoError(err)

	// In myStructInstance MyMap4 is nil and in myStructUnmarshaled MyMap4 is an
	// empty map
	require.Empty(myStructUnmarshaled.MyMap5)
	myStructUnmarshaled.MyMap5 = nil

	require.Zero(version)
	require.Equal(myStructInstance, *myStructUnmarshaled)
}

func TestRegisterStructTwice(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	require.NoError(cdec.RegisterType(&MyInnerStruct{}))
	err := cdec.RegisterType(&MyInnerStruct{})
	require.ErrorIs(err, codec.ErrDuplicateType)
}

func TestUInt32(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	number := uint32(500)

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, number)
	require.NoError(err)

	bytesLen, err := manager.Size(0, number)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var numberUnmarshaled uint32
	version, err := manager.Unmarshal(bytes, &numberUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(number, numberUnmarshaled)
}

func TestUIntPtr(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := codec.NewDefaultManager()

	require.NoError(manager.RegisterCodec(0, cdec))

	number := uintptr(500)
	_, err := manager.Marshal(0, number)
	require.ErrorIs(err, codec.ErrUnsupportedType)
}

func TestSlice(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := []bool{true, false, true, true}
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var sliceUnmarshaled []bool
	version, err := manager.Unmarshal(bytes, &sliceUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(mySlice, sliceUnmarshaled)
}

// Test marshalling/unmarshalling largest possible slice
func TestMaxSizeSlice(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := make([]string, math.MaxUint16)
	mySlice[0] = "first!"
	mySlice[math.MaxUint16-1] = "last!"
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var sliceUnmarshaled []string
	version, err := manager.Unmarshal(bytes, &sliceUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(mySlice, sliceUnmarshaled)
}

// Test marshalling a bool
func TestBool(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	myBool := true
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myBool)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myBool)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var boolUnmarshaled bool
	version, err := manager.Unmarshal(bytes, &boolUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myBool, boolUnmarshaled)
}

// Test marshalling an array
func TestArray(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	myArr := [5]uint64{5, 6, 7, 8, 9}
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myArr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myArr)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var myArrUnmarshaled [5]uint64
	version, err := manager.Unmarshal(bytes, &myArrUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myArr, myArrUnmarshaled)
}

// Test marshalling a really big array
func TestBigArray(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	myArr := [30000]uint64{5, 6, 7, 8, 9}
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myArr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myArr)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var myArrUnmarshaled [30000]uint64
	version, err := manager.Unmarshal(bytes, &myArrUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myArr, myArrUnmarshaled)
}

// Test marshalling a pointer to a struct
func TestPointerToStruct(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	myPtr := &MyInnerStruct{Str: "Hello!"}
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myPtr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myPtr)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var myPtrUnmarshaled *MyInnerStruct
	version, err := manager.Unmarshal(bytes, &myPtrUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myPtr, myPtrUnmarshaled)
}

// Test marshalling a slice of structs
func TestSliceOfStruct(cdec codec.GeneralCodec, t testing.TB) {
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
	require.NoError(cdec.RegisterType(&MyInnerStruct{}))

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var mySliceUnmarshaled []MyInnerStruct3
	version, err := manager.Unmarshal(bytes, &mySliceUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(mySlice, mySliceUnmarshaled)
}

// Test marshalling an interface
func TestInterface(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	require.NoError(cdec.RegisterType(&MyInnerStruct2{}))

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	var f Foo = &MyInnerStruct2{true}
	bytes, err := manager.Marshal(0, &f)
	require.NoError(err)

	bytesLen, err := manager.Size(0, &f)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var unmarshaledFoo Foo
	version, err := manager.Unmarshal(bytes, &unmarshaledFoo)
	require.NoError(err)
	require.Zero(version)
	require.Equal(f, unmarshaledFoo)
}

// Test marshalling a slice of interfaces
func TestSliceOfInterface(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	mySlice := []Foo{
		&MyInnerStruct{
			Str: "Hello",
		},
		&MyInnerStruct{
			Str: ", World!",
		},
	}
	require.NoError(cdec.RegisterType(&MyInnerStruct{}))

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, mySlice)
	require.NoError(err)

	bytesLen, err := manager.Size(0, mySlice)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var mySliceUnmarshaled []Foo
	version, err := manager.Unmarshal(bytes, &mySliceUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(mySlice, mySliceUnmarshaled)
}

// Test marshalling an array of interfaces
func TestArrayOfInterface(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	myArray := [2]Foo{
		&MyInnerStruct{
			Str: "Hello",
		},
		&MyInnerStruct{
			Str: ", World!",
		},
	}
	require.NoError(cdec.RegisterType(&MyInnerStruct{}))

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myArray)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myArray)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var myArrayUnmarshaled [2]Foo
	version, err := manager.Unmarshal(bytes, &myArrayUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myArray, myArrayUnmarshaled)
}

// Test marshalling a pointer to an interface
func TestPointerToInterface(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	var myinnerStruct Foo = &MyInnerStruct{Str: "Hello!"}
	myPtr := &myinnerStruct

	require.NoError(cdec.RegisterType(&MyInnerStruct{}))

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, &myPtr)
	require.NoError(err)

	bytesLen, err := manager.Size(0, &myPtr)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var myPtrUnmarshaled *Foo
	version, err := manager.Unmarshal(bytes, &myPtrUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myPtr, myPtrUnmarshaled)
}

// Test marshalling a string
func TestString(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	myString := "Ayy"
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myString)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myString)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var stringUnmarshaled string
	version, err := manager.Unmarshal(bytes, &stringUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myString, stringUnmarshaled)
}

// Ensure a nil slice is unmarshaled to slice with length 0
func TestNilSlice(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type structWithSlice struct {
		Slice []byte `serialize:"true"`
	}

	myStruct := structWithSlice{Slice: nil}
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, myStruct)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myStruct)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	var structUnmarshaled structWithSlice
	version, err := manager.Unmarshal(bytes, &structUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Empty(structUnmarshaled.Slice)
}

// Ensure that trying to serialize a struct with an unexported member
// that has `serialize:"true"` returns error
func TestSerializeUnexportedField(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type s struct {
		ExportedField   string `serialize:"true"`
		unexportedField string `serialize:"true"` //nolint:revive
	}

	myS := s{
		ExportedField:   "Hello, ",
		unexportedField: "world!",
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	_, err := manager.Marshal(0, myS)
	require.ErrorIs(err, codec.ErrUnexportedField)

	_, err = manager.Size(0, myS)
	require.ErrorIs(err, codec.ErrUnexportedField)
}

func TestSerializeOfNoSerializeField(cdec codec.GeneralCodec, t testing.TB) {
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
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	marshalled, err := manager.Marshal(0, myS)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myS)
	require.NoError(err)
	require.Len(marshalled, bytesLen)

	unmarshalled := s{}
	version, err := manager.Unmarshal(marshalled, &unmarshalled)
	require.NoError(err)
	require.Zero(version)

	expectedUnmarshalled := s{SerializedField: "Serialize me"}
	require.Equal(expectedUnmarshalled, unmarshalled)
}

// Test marshalling of nil slice
func TestNilSliceSerialization(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type simpleSliceStruct struct {
		Arr []uint32 `serialize:"true"`
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := &simpleSliceStruct{}
	expected := []byte{0, 0, 0, 0, 0, 0} // 0 for codec version, then nil slice marshaled as 0 length slice
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Len(result, bytesLen)

	valUnmarshaled := &simpleSliceStruct{}
	version, err := manager.Unmarshal(result, &valUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Empty(valUnmarshaled.Arr)
}

// Test marshaling a slice that has 0 elements (but isn't nil)
func TestEmptySliceSerialization(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type simpleSliceStruct struct {
		Arr []uint32 `serialize:"true"`
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := &simpleSliceStruct{Arr: make([]uint32, 0, 1)}
	expected := []byte{0, 0, 0, 0, 0, 0} // 0 for codec version (uint16) and 0 for size (uint32)
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Len(result, bytesLen)

	valUnmarshaled := &simpleSliceStruct{}
	version, err := manager.Unmarshal(result, &valUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(val, valUnmarshaled)
}

// Test marshaling empty slice of zero length structs
func TestSliceWithEmptySerialization(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type emptyStruct struct{}

	type nestedSliceStruct struct {
		Arr []emptyStruct `serialize:"true"`
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := &nestedSliceStruct{
		Arr: make([]emptyStruct, 0),
	}
	expected := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // codec version (0x00, 0x00) then (0x00, 0x00, 0x00, 0x00) for numElts
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Len(result, bytesLen)

	unmarshaled := nestedSliceStruct{}
	version, err := manager.Unmarshal(expected, &unmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Empty(unmarshaled.Arr)
}

func TestSliceWithEmptySerializationError(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type emptyStruct struct{}

	type nestedSliceStruct struct {
		Arr []emptyStruct `serialize:"true"`
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := &nestedSliceStruct{
		Arr: make([]emptyStruct, 1),
	}
	_, err := manager.Marshal(0, val)
	require.ErrorIs(err, codec.ErrMarshalZeroLength)

	_, err = manager.Size(0, val)
	require.ErrorIs(err, codec.ErrMarshalZeroLength)

	b := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01} // codec version (0x00, 0x00) then (0x00, 0x00, 0x00, 0x01) for numElts

	unmarshaled := nestedSliceStruct{}
	_, err = manager.Unmarshal(b, &unmarshaled)
	require.ErrorIs(err, codec.ErrUnmarshalZeroLength)
}

// Test marshaling empty map of zero length structs
func TestMapWithEmptySerialization(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type emptyStruct struct{}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := make(map[emptyStruct]emptyStruct)
	expected := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // codec version (0x00, 0x00) then (0x00, 0x00, 0x00, 0x00) for numElts
	result, err := manager.Marshal(0, val)
	require.NoError(err)
	require.Equal(expected, result)

	bytesLen, err := manager.Size(0, val)
	require.NoError(err)
	require.Len(result, bytesLen)

	var unmarshaled map[emptyStruct]emptyStruct
	version, err := manager.Unmarshal(expected, &unmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Empty(unmarshaled)
}

func TestMapWithEmptySerializationError(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type emptyStruct struct{}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := map[emptyStruct]emptyStruct{
		{}: {},
	}
	_, err := manager.Marshal(0, val)
	require.ErrorIs(err, codec.ErrMarshalZeroLength)

	_, err = manager.Size(0, val)
	require.ErrorIs(err, codec.ErrMarshalZeroLength)

	b := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01} // codec version (0x00, 0x00) then (0x00, 0x00, 0x00, 0x01) for numElts

	var unmarshaled map[emptyStruct]emptyStruct
	_, err = manager.Unmarshal(b, &unmarshaled)
	require.ErrorIs(err, codec.ErrUnmarshalZeroLength)
}

func TestSliceTooLarge(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	val := []struct{}{}
	b := []byte{0x00, 0x00, 0xff, 0xff, 0xff, 0xff}
	_, err := manager.Unmarshal(b, &val)
	require.ErrorIs(err, codec.ErrMaxSliceLenExceeded)
}

// Ensure serializing structs with negative number members works
func TestNegativeNumbers(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type s struct {
		MyInt8  int8  `serialize:"true"`
		MyInt16 int16 `serialize:"true"`
		MyInt32 int32 `serialize:"true"`
		MyInt64 int64 `serialize:"true"`
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	myS := s{-1, -2, -3, -4}
	bytes, err := manager.Marshal(0, myS)
	require.NoError(err)

	bytesLen, err := manager.Size(0, myS)
	require.NoError(err)
	require.Len(bytes, bytesLen)

	mySUnmarshaled := s{}
	version, err := manager.Unmarshal(bytes, &mySUnmarshaled)
	require.NoError(err)
	require.Zero(version)
	require.Equal(myS, mySUnmarshaled)
}

// Ensure deserializing structs with too many bytes errors correctly
func TestTooLargeUnmarshal(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type inner struct {
		B uint16 `serialize:"true"`
	}
	bytes := []byte{0, 0, 0, 0}

	manager := codec.NewManager(3)
	require.NoError(manager.RegisterCodec(0, cdec))

	s := inner{}
	_, err := manager.Unmarshal(bytes, &s)
	require.ErrorIs(err, codec.ErrUnmarshalTooBig)
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
func TestUnmarshalInvalidInterface(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := codec.NewDefaultManager()
	require.NoError(cdec.RegisterType(&innerInterface{}))
	require.NoError(cdec.RegisterType(&innerNoInterface{}))
	require.NoError(manager.RegisterCodec(0, cdec))

	{
		bytes := []byte{0, 0, 0, 0, 0, 0}
		s := outer{}
		version, err := manager.Unmarshal(bytes, &s)
		require.NoError(err)
		require.Zero(version)
	}
	{
		bytes := []byte{0, 0, 0, 0, 0, 1}
		s := outer{}
		_, err := manager.Unmarshal(bytes, &s)
		require.ErrorIs(err, codec.ErrDoesNotImplementInterface)
	}
}

// Test unmarshaling something with extra data
func TestExtraSpace(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	// codec version 0x0000 then 0x01 for b then 0x02 as extra data.
	byteSlice := []byte{0x00, 0x00, 0x01, 0x02}
	var b byte
	_, err := manager.Unmarshal(byteSlice, &b)
	require.ErrorIs(err, codec.ErrExtraSpace)
}

// Ensure deserializing slices whose lengths exceed MaxInt32 error correctly
func TestSliceLengthOverflow(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	type inner struct {
		Vals []uint32 `serialize:"true"`
	}
	bytes := []byte{
		// Codec Version:
		0x00, 0x00,
		// Slice Length:
		0xff, 0xff, 0xff, 0xff,
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	s := inner{}
	_, err := manager.Unmarshal(bytes, &s)
	require.ErrorIs(err, codec.ErrMaxSliceLenExceeded)
}

type MultipleVersionsStruct struct {
	BothTags    string `tag1:"true"  tag2:"true"`
	SingleTag1  string `tag1:"true"`
	SingleTag2  string `             tag2:"true"`
	EitherTags1 string `tag1:"false" tag2:"true"`
	EitherTags2 string `tag1:"true"  tag2:"false"`
	NoTags      string `tag1:"false" tag2:"false"`
}

func TestMultipleTags(cdec codec.GeneralCodec, t testing.TB) {
	// received codec is expected to have both v1 and v2 registered as tags
	inputs := MultipleVersionsStruct{
		BothTags:    "both Tags",
		SingleTag1:  "Only Tag1",
		SingleTag2:  "Only Tag2",
		EitherTags1: "Tag2 is false",
		EitherTags2: "Tag1 is false",
		NoTags:      "Neither Tag",
	}

	manager := codec.NewDefaultManager()
	for _, codecVersion := range []uint16{0, 1, 2022} {
		require := require.New(t)

		require.NoError(manager.RegisterCodec(codecVersion, cdec))

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

func TestMap(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	data1 := map[string]MyInnerStruct2{
		"test": {true},
		"bar":  {false},
	}

	data2 := map[string]MyInnerStruct2{
		"bar":  {false},
		"test": {true},
	}

	data3 := map[string]MyInnerStruct2{
		"bar": {false},
	}

	outerMap := make(map[int32]map[string]MyInnerStruct2)
	outerMap[3] = data1
	outerMap[19] = data2

	outerArray := [3]map[string]MyInnerStruct2{
		data1,
		data2,
		data3,
	}

	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(0, cdec))

	data1Bytes, err := manager.Marshal(0, data1)
	require.NoError(err)

	// data1 and data2 should have the same byte representation even though
	// their key-value pairs were defined in a different order.
	data2Bytes, err := manager.Marshal(0, data2)
	require.NoError(err)
	require.Equal(data1Bytes, data2Bytes)

	// Make sure Size returns the correct size for the marshalled data
	data1Size, err := manager.Size(0, data1)
	require.NoError(err)
	require.Len(data1Bytes, data1Size)

	var unmarshalledData1 map[string]MyInnerStruct2
	_, err = manager.Unmarshal(data1Bytes, &unmarshalledData1)
	require.NoError(err)
	require.Equal(data1, unmarshalledData1)

	outerMapBytes, err := manager.Marshal(0, outerMap)
	require.NoError(err)

	outerMapSize, err := manager.Size(0, outerMap)
	require.NoError(err)
	require.Len(outerMapBytes, outerMapSize)

	var unmarshalledOuterMap map[int32]map[string]MyInnerStruct2
	_, err = manager.Unmarshal(outerMapBytes, &unmarshalledOuterMap)
	require.NoError(err)
	require.Equal(outerMap, unmarshalledOuterMap)

	outerArrayBytes, err := manager.Marshal(0, outerArray)
	require.NoError(err)

	outerArraySize, err := manager.Size(0, outerArray)
	require.NoError(err)
	require.Len(outerArrayBytes, outerArraySize)
}

func TestCanMarshalLargeSlices(cdec codec.GeneralCodec, t testing.TB) {
	require := require.New(t)

	data := make([]uint16, 1_000_000)

	manager := codec.NewManager(math.MaxInt)
	require.NoError(manager.RegisterCodec(0, cdec))

	bytes, err := manager.Marshal(0, data)
	require.NoError(err)

	var unmarshalledData []uint16
	_, err = manager.Unmarshal(bytes, &unmarshalledData)
	require.NoError(err)
	require.Equal(data, unmarshalledData)
}

func FuzzStructUnmarshal(cdec codec.GeneralCodec, f *testing.F) {
	manager := codec.NewDefaultManager()
	// Register the types that may be unmarshaled into interfaces
	require.NoError(f, cdec.RegisterType(&MyInnerStruct{}))
	require.NoError(f, cdec.RegisterType(&MyInnerStruct2{}))
	require.NoError(f, cdec.RegisterType(""))
	require.NoError(f, cdec.RegisterType(int32(0)))
	require.NoError(f, manager.RegisterCodec(0, cdec))

	f.Fuzz(func(t *testing.T, bytes []byte) {
		require := require.New(t)

		myParsedStruct := &myStruct{}
		version, err := manager.Unmarshal(bytes, myParsedStruct)
		if err != nil {
			return
		}
		require.Zero(version)

		marshalled, err := manager.Marshal(version, myParsedStruct)
		require.NoError(err)
		require.Equal(bytes, marshalled)

		size, err := manager.Size(version, myParsedStruct)
		require.NoError(err)
		require.Len(bytes, size)
	})
}