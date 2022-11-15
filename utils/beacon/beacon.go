// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beacon

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var _ Beacon = (*beacon)(nil)

type Beacon interface {
	ID() ids.NodeID
	IP() ips.IPPort
}

type beacon struct {
	id ids.NodeID
	ip ips.IPPort
}

func New(id ids.NodeID, ip ips.IPPort) Beacon {
	return &beacon{
		id: id,
		ip: ip,
	}
}

func (b *beacon) ID() ids.NodeID {
	return b.id
}

func (b *beacon) IP() ips.IPPort {
	return b.ip
}
