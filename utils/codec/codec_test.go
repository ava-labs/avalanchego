// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// The below structs and interfaces exist
// for the sake of testing

type Foo interface {
	Foo() int
}

// *MyInnerStruct implements Foo
type MyInnerStruct struct {
	Str string `serialize:"true"`
}

func (m *MyInnerStruct) Foo() int {
	return 1
}

// *MyInnerStruct2 implements Foo
type MyInnerStruct2 struct {
	Bool bool `serialize:"true"`
}

func (m *MyInnerStruct2) Foo() int {
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
func TestStruct(t *testing.T) {
	temp := Foo(&MyInnerStruct{})
	myStructInstance := myStruct{
		InnerStruct:  MyInnerStruct{"hello"},
		InnerStruct2: &MyInnerStruct{"yello"},
		Member1:      1,
		Member2:      2,
		MySlice:      []byte{1, 2, 3, 4},
		MySlice2:     []string{"one", "two", "three"},
		MySlice3:     []MyInnerStruct{{"a"}, {"b"}, {"c"}},
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

	codec := NewDefault()
	manager := NewDefaultManager()
	errs := wrappers.Errs{}
	errs.Add(
		codec.RegisterType(&MyInnerStruct{}), // Register the types that may be unmarshaled into interfaces
		codec.RegisterType(&MyInnerStruct2{}),
		manager.RegisterCodec(0, codec),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	myStructBytes, err := manager.Marshal(0, myStructInstance)
	if err != nil {
		t.Fatal(err)
	}

	myStructUnmarshaled := &myStruct{}
	err = codec.Unmarshal(myStructBytes, myStructUnmarshaled)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(*myStructUnmarshaled, myStructInstance) {
		t.Fatal("should be same")
	}
}

func TestUInt32(t *testing.T) {
	number := uint32(500)

	codec := NewDefault()
	manager := NewDefaultManager()
	if err := manager.RegisterCodec(0, codec); err != nil {
		t.Fatal(err)
	}

	bytes, err := manager.Marshal(0, number)
	if err != nil {
		t.Fatal(err)
	}

	var numberUnmarshaled uint32
	version, err := manager.Unmarshal(bytes, &numberUnmarshaled)
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatalf("wrong version returned. Expected %d ; Returned %d", 0, version)
	}

	if number != numberUnmarshaled {
		t.Fatal("expected marshaled and unmarshaled values to match")
	}
}

func TestSlice(t *testing.T) {
	mySlice := []bool{true, false, true, true}
	codec := NewDefault()
	bytes, err := codec.Marshal(mySlice)
	if err != nil {
		t.Fatal(err)
	}

	var sliceUnmarshaled []bool
	if err := codec.Unmarshal(bytes, &sliceUnmarshaled); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(mySlice, sliceUnmarshaled) {
		t.Fatal("expected marshaled and unmarshaled values to match")
	}
}

// // Test marshalling/unmarshalling largest possible slice
// func TestMaxSizeSlice(t *testing.T) {
// 	mySlice := make([]string, math.MaxUint16)
// 	mySlice[0] = "first!"
// 	mySlice[math.MaxUint16-1] = "last!"
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(mySlice)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var sliceUnmarshaled []string
// 	if err := codec.Unmarshal(bytes, &sliceUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(mySlice, sliceUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling a bool
// func TestBool(t *testing.T) {
// 	myBool := true
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(myBool)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var boolUnmarshaled bool
// 	if err := codec.Unmarshal(bytes, &boolUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myBool, boolUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling an array
// func TestArray(t *testing.T) {
// 	myArr := [5]uint64{5, 6, 7, 8, 9}
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(myArr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var myArrUnmarshaled [5]uint64
// 	if err := codec.Unmarshal(bytes, &myArrUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myArr, myArrUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling a really big array
// func TestBigArray(t *testing.T) {
// 	myArr := [30000]uint64{5, 6, 7, 8, 9}
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(myArr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var myArrUnmarshaled [30000]uint64
// 	if err := codec.Unmarshal(bytes, &myArrUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myArr, myArrUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling a pointer to a struct
// func TestPointerToStruct(t *testing.T) {
// 	myPtr := &MyInnerStruct{Str: "Hello!"}
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(myPtr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var myPtrUnmarshaled *MyInnerStruct
// 	if err := codec.Unmarshal(bytes, &myPtrUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myPtr, myPtrUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling a slice of structs
// func TestSliceOfStruct(t *testing.T) {
// 	mySlice := []MyInnerStruct3{
// 		{
// 			Str: "One",
// 			M1:  MyInnerStruct{"Two"},
// 			F:   &MyInnerStruct{"Three"},
// 		},
// 		{
// 			Str: "Four",
// 			M1:  MyInnerStruct{"Five"},
// 			F:   &MyInnerStruct{"Six"},
// 		},
// 	}
// 	codec := NewDefault()
// 	if err := codec.RegisterType(&MyInnerStruct{}); err != nil {
// 		t.Fatal(err)
// 	}
// 	bytes, err := codec.Marshal(mySlice)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var mySliceUnmarshaled []MyInnerStruct3
// 	if err := codec.Unmarshal(bytes, &mySliceUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(mySlice, mySliceUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling an interface
// func TestInterface(t *testing.T) {
// 	codec := NewDefault()
// 	if err := codec.RegisterType(&MyInnerStruct2{}); err != nil {
// 		t.Fatal(err)
// 	}

// 	var f Foo = &MyInnerStruct2{true}
// 	bytes, err := codec.Marshal(&f)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var unmarshaledFoo Foo
// 	err = codec.Unmarshal(bytes, &unmarshaledFoo)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(f, unmarshaledFoo) {
// 		t.Fatal("expected unmarshaled value to match original")
// 	}
// }

// // Test marshalling a slice of interfaces
// func TestSliceOfInterface(t *testing.T) {
// 	mySlice := []Foo{
// 		&MyInnerStruct{
// 			Str: "Hello",
// 		},
// 		&MyInnerStruct{
// 			Str: ", World!",
// 		},
// 	}
// 	codec := NewDefault()
// 	if err := codec.RegisterType(&MyInnerStruct{}); err != nil {
// 		t.Fatal(err)
// 	}
// 	bytes, err := codec.Marshal(mySlice)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var mySliceUnmarshaled []Foo
// 	if err := codec.Unmarshal(bytes, &mySliceUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(mySlice, mySliceUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling an array of interfaces
// func TestArrayOfInterface(t *testing.T) {
// 	myArray := [2]Foo{
// 		&MyInnerStruct{
// 			Str: "Hello",
// 		},
// 		&MyInnerStruct{
// 			Str: ", World!",
// 		},
// 	}
// 	codec := NewDefault()
// 	if err := codec.RegisterType(&MyInnerStruct{}); err != nil {
// 		t.Fatal(err)
// 	}
// 	bytes, err := codec.Marshal(myArray)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var myArrayUnmarshaled [2]Foo
// 	if err := codec.Unmarshal(bytes, &myArrayUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myArray, myArrayUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling a pointer to an interface
// func TestPointerToInterface(t *testing.T) {
// 	var myinnerStruct Foo = &MyInnerStruct{Str: "Hello!"}
// 	var myPtr *Foo = &myinnerStruct

// 	codec := NewDefault()
// 	if err := codec.RegisterType(&MyInnerStruct{}); err != nil {
// 		t.Fatal(err)
// 	}

// 	bytes, err := codec.Marshal(&myPtr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var myPtrUnmarshaled *Foo
// 	if err := codec.Unmarshal(bytes, &myPtrUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myPtr, myPtrUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Test marshalling a string
// func TestString(t *testing.T) {
// 	myString := "Ayy"
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(myString)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var stringUnmarshaled string
// 	if err := codec.Unmarshal(bytes, &stringUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myString, stringUnmarshaled) {
// 		t.Fatal("expected marshaled and unmarshaled values to match")
// 	}
// }

// // Ensure a nil slice is unmarshaled to slice with length 0
// func TestNilSlice(t *testing.T) {
// 	type structWithSlice struct {
// 		Slice []byte `serialize:"true"`
// 	}

// 	myStruct := structWithSlice{Slice: nil}
// 	codec := NewDefault()
// 	bytes, err := codec.Marshal(myStruct)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	var structUnmarshaled structWithSlice
// 	if err := codec.Unmarshal(bytes, &structUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if structUnmarshaled.Slice == nil || len(structUnmarshaled.Slice) != 0 {
// 		t.Fatal("expected slice to be non-nil and length 0")
// 	}
// }

// // Ensure that trying to serialize a struct with an unexported member
// // that has `serialize:"true"` returns error
// func TestSerializeUnexportedField(t *testing.T) {
// 	type s struct {
// 		ExportedField   string `serialize:"true"`
// 		unexportedField string `serialize:"true"`
// 	}

// 	myS := s{
// 		ExportedField:   "Hello, ",
// 		unexportedField: "world!",
// 	}

// 	codec := NewDefault()
// 	if _, err := codec.Marshal(myS); err == nil {
// 		t.Fatalf("expected err but got none")
// 	}
// }

// func TestSerializeOfNoSerializeField(t *testing.T) {
// 	type s struct {
// 		SerializedField   string `serialize:"true"`
// 		UnserializedField string `serialize:"false"`
// 		UnmarkedField     string
// 	}
// 	myS := s{
// 		SerializedField:   "Serialize me",
// 		UnserializedField: "Do not serialize me",
// 		UnmarkedField:     "No declared serialize",
// 	}
// 	codec := NewDefault()
// 	marshalled, err := codec.Marshal(myS)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	unmarshalled := s{}
// 	err = codec.Unmarshal(marshalled, &unmarshalled)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	expectedUnmarshalled := s{SerializedField: "Serialize me"}
// 	if !reflect.DeepEqual(unmarshalled, expectedUnmarshalled) {
// 		t.Fatalf("Got %#v, expected %#v", unmarshalled, expectedUnmarshalled)
// 	}
// }

// type simpleSliceStruct struct {
// 	Arr []uint32 `serialize:"true"`
// }

// // Test marshalling of nil slice
// func TestNilSliceSerialization(t *testing.T) {
// 	codec := NewDefault()
// 	val := &simpleSliceStruct{}
// 	expected := []byte{0, 0, 0, 0, 0, 0} // 0 for codec version, then nil slice marshaled as 0 length slice
// 	result, err := codec.Marshal(val)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if !bytes.Equal(expected, result) {
// 		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
// 	}
// 	valUnmarshaled := &simpleSliceStruct{}
// 	if err = codec.Unmarshal(result, &valUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	} else if len(valUnmarshaled.Arr) != 0 {
// 		t.Fatal("should be 0 length")
// 	}
// }

// // Test marshaling a slice that has 0 elements (but isn't nil)
// func TestEmptySliceSerialization(t *testing.T) {
// 	codec := NewDefault()

// 	val := &simpleSliceStruct{Arr: make([]uint32, 0, 1)}
// 	expected := []byte{0, 0, 0, 0, 0, 0} // 0 for codec version (uint16) and 0 for size (uint32)
// 	result, err := codec.Marshal(val)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if !bytes.Equal(expected, result) {
// 		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
// 	}

// 	valUnmarshaled := &simpleSliceStruct{}
// 	if err = codec.Unmarshal(result, &valUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	} else if !reflect.DeepEqual(valUnmarshaled, val) {
// 		t.Fatal("should be same")
// 	}
// }

// type emptyStruct struct{}

// type nestedSliceStruct struct {
// 	Arr []emptyStruct `serialize:"true"`
// }

// // Test marshaling slice that is not nil and not empty
// func TestSliceWithEmptySerialization(t *testing.T) {
// 	codec := NewDefault()

// 	val := &nestedSliceStruct{
// 		Arr: make([]emptyStruct, 1000),
// 	}
// 	expected := []byte{0x00, 0x00, 0x00, 0x00, 0x03, 0xE8} // codec version (0x00, 0x00) then 1000 for numElts
// 	result, err := codec.Marshal(val)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if !bytes.Equal(expected, result) {
// 		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
// 	}

// 	unmarshaled := nestedSliceStruct{}
// 	if err := codec.Unmarshal(expected, &unmarshaled); err != nil {
// 		t.Fatal(err)
// 	}
// 	if len(unmarshaled.Arr) != 1000 {
// 		t.Fatalf("Should have created a slice of length %d", 1000)
// 	}
// }

// func TestSliceWithEmptySerializationOutOfMemory(t *testing.T) {
// 	codec := NewDefault()

// 	val := &nestedSliceStruct{
// 		Arr: make([]emptyStruct, defaultMaxSliceLength+1),
// 	}
// 	bytes, err := codec.Marshal(val)
// 	if err == nil {
// 		t.Fatal("should have failed due to slice length too large")
// 	}

// 	unmarshaled := nestedSliceStruct{}
// 	if err := codec.Unmarshal(bytes, &unmarshaled); err == nil {
// 		t.Fatalf("Should have errored due to excess memory requested")
// 	}
// }

// func TestOutOfMemory(t *testing.T) {
// 	codec := NewDefault()

// 	val := []bool{}
// 	b := []byte{0xff, 0xff, 0xff, 0xff, 0x00}
// 	if err := codec.Unmarshal(b, &val); err == nil {
// 		t.Fatalf("Should have errored due to memory usage")
// 	}
// }

// // Ensure serializing structs with negative number members works
// func TestNegativeNumbers(t *testing.T) {
// 	type s struct {
// 		MyInt8  int8  `serialize:"true"`
// 		MyInt16 int16 `serialize:"true"`
// 		MyInt32 int32 `serialize:"true"`
// 		MyInt64 int64 `serialize:"true"`
// 	}

// 	myS := s{-1, -2, -3, -4}

// 	codec := NewDefault()

// 	bytes, err := codec.Marshal(myS)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	mySUnmarshaled := s{}
// 	if err := codec.Unmarshal(bytes, &mySUnmarshaled); err != nil {
// 		t.Fatal(err)
// 	}

// 	if !reflect.DeepEqual(myS, mySUnmarshaled) {
// 		t.Log(mySUnmarshaled)
// 		t.Log(myS)
// 		t.Fatal("expected marshaled and unmarshaled structs to be the same")
// 	}
// }

// // Ensure deserializing structs with too many bytes errors correctly
// func TestTooLargeUnmarshal(t *testing.T) {
// 	type inner struct {
// 		Long uint64 `serialize:"true"`
// 	}
// 	bytes := []byte{0, 0, 0, 0}
// 	s := inner{}
// 	codec := New(3, 1)
// 	err := codec.Unmarshal(bytes, &s)
// 	if err == nil {
// 		t.Fatalf("Should have errored due to too many bytes provided")
// 	}
// }

// type outerInterface interface {
// 	ToInt() int
// }

// type outer struct {
// 	Interface outerInterface `serialize:"true"`
// }

// type innerInterface struct{}

// func (it *innerInterface) ToInt() int {
// 	return 0
// }

// type innerNoInterface struct{}

// // Ensure deserializing structs into the wrong interface errors gracefully
// func TestUnmarshalInvalidInterface(t *testing.T) {
// 	codec := NewDefault()

// 	if err := codec.RegisterType(&innerInterface{}); err != nil {
// 		t.Fatal(err)
// 	}
// 	if err := codec.RegisterType(&innerNoInterface{}); err != nil {
// 		t.Fatal(err)
// 	}

// 	{
// 		bytes := []byte{0, 0, 0, 0, 0, 0}
// 		s := outer{}
// 		if err := codec.Unmarshal(bytes, &s); err != nil {
// 			t.Fatal(err)
// 		}
// 	}
// 	{
// 		bytes := []byte{0, 0, 0, 0, 0, 1}
// 		s := outer{}
// 		if err := codec.Unmarshal(bytes, &s); err == nil {
// 			t.Fatalf("should have errored")
// 		}
// 	}
// }
