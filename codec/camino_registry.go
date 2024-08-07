// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

// Registry registers new types that can be marshaled into
type CaminoRegistry interface {
	RegisterType(interface{}) error
	RegisterCustomType(interface{}) error
}
