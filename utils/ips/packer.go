// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import "github.com/ava-labs/avalanchego/utils/wrappers"

type Packer struct {
	wrappers.Packer
}

// PackIP packs an ip port pair to the byte array
func (p *Packer) PackIP(ip IPPort) {
	p.PackFixedBytes(ip.IP.To16())
	p.PackShort(ip.Port)
}
