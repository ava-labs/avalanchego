// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"errors"

	"github.com/ava-labs/libevm/libevm"
)

// ErrNoJSONCodec is returned when [NetworkUpgrades] are (un)marshalled before
// a codec is registered with [RegisterJSON].
var ErrNoJSONCodec = errors.New("no NetworkUpgrades JSON codec registered")

// A NetworkUpgradesJSON defines how a VM serializes [NetworkUpgrades].
type NetworkUpgradesJSON interface {
	// Marshal returns the JSON object encoding n.
	Marshal(n *NetworkUpgrades) ([]byte, error)
	// Unmarshal decodes the JSON object in data into n. The object MAY contain
	// unrelated keys, which MUST be ignored.
	Unmarshal(data []byte, n *NetworkUpgrades) error
}

var jsonCodec NetworkUpgradesJSON

// RegisterJSON registers the codec used by [NetworkUpgrades.MarshalJSON] and
// [NetworkUpgrades.UnmarshalJSON]. It MUST NOT be called more than once and
// therefore is only allowed to be used in tests and `package main`, to avoid
// polluting other packages that transitively depend on this one but don't
// need registration.
func RegisterJSON(c NetworkUpgradesJSON) {
	if jsonCodec != nil {
		panic("NetworkUpgrades JSON codec already registered")
	}
	jsonCodec = c
}

// WithTempRegisteredJSON runs `fn` with `c` temporarily registered, otherwise
// equivalent to a call to [RegisterJSON], but limited to the life of `fn`.
func WithTempRegisteredJSON(lock libevm.ExtrasLock, c NetworkUpgradesJSON, fn func() error) error {
	if err := lock.Verify(); err != nil {
		return err
	}
	old := jsonCodec
	defer func() { jsonCodec = old }()

	jsonCodec = c
	return fn()
}

// MarshalJSON implements [json.Marshaler] using the codec registered with
// [RegisterJSON].
func (n NetworkUpgrades) MarshalJSON() ([]byte, error) {
	if jsonCodec == nil {
		return nil, ErrNoJSONCodec
	}
	return jsonCodec.Marshal(&n)
}

// UnmarshalJSON implements [json.Unmarshaler] using the codec registered with
// [RegisterJSON].
func (n *NetworkUpgrades) UnmarshalJSON(data []byte) error {
	if jsonCodec == nil {
		return ErrNoJSONCodec
	}
	return jsonCodec.Unmarshal(data, n)
}
