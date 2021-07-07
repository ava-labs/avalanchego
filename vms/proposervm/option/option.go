// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"github.com/ava-labs/avalanchego/ids"
)

type Option interface {
	ID() ids.ID
	ParentID() ids.ID
	CoreBlock() []byte
	Bytes() []byte
}

type option struct {
	id         ids.ID
	PrntID     ids.ID `serialize:"true"`
	InnerBytes []byte `serialize:"true"`
	bytes      []byte
}

func (b *option) ID() ids.ID        { return b.id }
func (b *option) ParentID() ids.ID  { return b.PrntID }
func (b *option) CoreBlock() []byte { return b.InnerBytes }
func (b *option) Bytes() []byte     { return b.bytes }
