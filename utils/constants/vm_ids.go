// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/ava-labs/avalanchego/ids"

const (
	PlatformVMName = "platformvm"
	AVMName        = "avm"
	EVMName        = "evm"
	SubnetEVMName  = "subnetevm"
	XSVMName       = "xsvm"
)

var (
	PlatformVMID = ids.ID{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'}
	AVMID        = ids.ID{'a', 'v', 'm'}
	EVMID        = ids.ID{'e', 'v', 'm'}
	SubnetEVMID  = ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'e', 'v', 'm'}
	XSVMID       = ids.ID{'x', 's', 'v', 'm'}
	StrEVMID     = ids.ID{'s', 't', 'r', 'e', 'v', 'm'}
)

// VMName returns the name of the VM with the provided ID. If a human readable
// name isn't known, then the formatted ID is returned.
func VMName(vmID ids.ID) string {
	switch vmID {
	case PlatformVMID:
		return PlatformVMName
	case AVMID:
		return AVMName
	case EVMID:
		return EVMName
	case SubnetEVMID:
		return SubnetEVMName
	case XSVMID:
		return XSVMName
	default:
		return vmID.String()
	}
}
