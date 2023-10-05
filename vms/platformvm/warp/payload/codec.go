// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var errWrongType = errors.New("wrong payload type")

const (
	codecVersion = 0

	MaxMessageSize = 24 * units.KiB

	// Note: Modifying this variable can have subtle implications on memory
	// usage when parsing malformed payloads.
	MaxSliceLen = 24 * units.KiB
)

// Codec does serialization and deserialization for Warp messages.
var c codec.Manager

func init() {
	c = codec.NewManager(MaxMessageSize)
	lc := linearcodec.NewCustomMaxLength(MaxSliceLen)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&AddressedCall{}),
		lc.RegisterType(&BlockHash{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// byteSetter provides an interface to set the bytes of an underlying type to [b]
// after unmarshalling into that type.
type byteSetter interface {
	setBytes(b []byte)
}

func Parse(bytes []byte) (byteSetter, error) {
	var intf interface{}
	if _, err := c.Unmarshal(bytes, &intf); err != nil {
		return nil, err
	}

	payload, ok := intf.(byteSetter)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, intf)
	}
	payload.setBytes(bytes)
	return payload, nil
}
