// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

// GeneralCodec marshals and unmarshals structs including interfaces
type GeneralCodec interface {
	Codec
	Registry
}
