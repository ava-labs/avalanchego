package graft

import (
	"fmt"

	"github.com/ava-labs/libevm/libevm/pseudo"
)

const EVIL_EXPORTED_CONSTANT = "hi"

func Bad() {
	pseudo.NewValue[int](nil)
	fmt.Println(EVIL_EXPORTED_CONSTANT)
}
