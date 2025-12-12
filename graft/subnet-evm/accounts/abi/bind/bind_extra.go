// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bind

import (
	"fmt"
	"regexp"

	"github.com/ava-labs/subnet-evm/accounts/abi"
)

type (
	// These types are exported for use in bind/precompilebind
	TmplContract = tmplContract
	TmplMethod   = tmplMethod
	TmplStruct   = tmplStruct
)

// BindHook is a callback function that can be used to customize the binding.
type BindHook func(lang Lang, pkg string, types []string, contracts map[string]*tmplContract, structs map[string]*tmplStruct) (data any, templateSource string, err error)

func IsKeyWord(arg string) bool {
	return isKeyWord(arg)
}

var bindTypeNew = map[Lang]func(kind abi.Type, structs map[string]*tmplStruct) string{
	LangGo: bindTypeNewGo,
}

// bindTypeNewGo converts new types to Go ones.
func bindTypeNewGo(kind abi.Type, structs map[string]*tmplStruct) string {
	switch kind.T {
	case abi.TupleTy:
		return structs[kind.TupleRawName+kind.String()].Name + "{}"
	case abi.ArrayTy:
		return fmt.Sprintf("[%d]", kind.Size) + bindTypeGo(*kind.Elem, structs) + "{}"
	case abi.SliceTy:
		return "nil"
	case abi.AddressTy:
		return "common.Address{}"
	case abi.IntTy, abi.UintTy:
		parts := regexp.MustCompile(`(u)?int([0-9]*)`).FindStringSubmatch(kind.String())
		switch parts[2] {
		case "8", "16", "32", "64":
			return "0"
		}
		return "new(big.Int)"
	case abi.FixedBytesTy:
		return fmt.Sprintf("[%d]byte", kind.Size) + "{}"
	case abi.BytesTy:
		return "[]byte{}"
	case abi.FunctionTy:
		return "[24]byte{}"
	case abi.BoolTy:
		return "false"
	case abi.StringTy:
		return `""`
	default:
		return "nil"
	}
}

func mkList(args ...any) []any {
	return args
}

func add(a, b int) int {
	return a + b
}
