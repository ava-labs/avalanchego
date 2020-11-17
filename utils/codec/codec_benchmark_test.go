// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// BenchmarkMarshal benchmarks the codec's marshal function
func BenchmarkMarshal(b *testing.B) {
	temp := Foo(&MyInnerStruct{})
	myStructInstance := myStruct{
		InnerStruct:  MyInnerStruct{"hello"},
		InnerStruct2: &MyInnerStruct{"yello"},
		Member1:      1,
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
	var unmarshaledMyStructInstance myStruct

	codec := NewDefault()
	manager := NewDefaultManager()
	errs := wrappers.Errs{}
	errs.Add(
		codec.RegisterType(&MyInnerStruct{}), // Register the types that may be unmarshaled into interfaces
		codec.RegisterType(&MyInnerStruct2{}),
		manager.RegisterCodec(0, codec),
	)
	_, err := manager.Marshal(0, myStructInstance) // warm up serializedFields cache
	if errs.Add(err); errs.Errored() {
		b.Fatal(errs.Err)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bytes, err := manager.Marshal(0, myStructInstance)
		if err != nil {
			b.Fatal(err)
		}
		version, err := manager.Unmarshal(bytes, &unmarshaledMyStructInstance)
		if err != nil {
			b.Fatal(err)
		}
		if version != 0 {
			b.Fatalf("wrong version returned. Expected %d ; Returned %d", 0, version)
		}

	}
}

func BenchmarkMarshalNonCodec(b *testing.B) {
	p := wrappers.Packer{}
	for n := 0; n < b.N; n++ {
		for i := 0; i < 30; i++ {
			p.PackStr("yay")
		}
	}
}
