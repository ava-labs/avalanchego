// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import "errors"

var ErrDuplicateType = errors.New("duplicate type registration")

// Registry registers new types that can be marshaled into
type Registry interface {
	RegisterType(interface{}) error
}
