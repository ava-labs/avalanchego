// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

type Unsigned interface {
	Visit(Visitor) error
}
