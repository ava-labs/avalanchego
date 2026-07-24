// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	ErrUnsupportedType           = errors.New("unsupported type")
	ErrMaxSliceLenExceeded       = errors.New("max slice length exceeded")
	ErrDoesNotImplementInterface = errors.New("does not implement interface")
	ErrUnexportedField           = errors.New("unexported field")
	ErrMarshalZeroLength         = errors.New("can't marshal zero length value")
	ErrUnmarshalZeroLength       = errors.New("can't unmarshal zero length value")
)

// Codec marshals and unmarshals
type Codec interface {
	MarshalInto(any, *wrappers.Packer) error
	UnmarshalFrom(*wrappers.Packer, any) error

	// Returns the size, in bytes, of [value] when it's marshaled
	Size(value any) (int, error)
}
