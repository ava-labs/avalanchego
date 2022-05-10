// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"crypto/x509"
)

// Can't import these from wrappers package due to circular import.
const (
	intLen  = 4
	longLen = 8
	ipLen   = 18
	// Certificate length, signature length, IP, timestamp
	baseIPCertDescLen = 2*intLen + ipLen + longLen
)

type IPCertDesc struct {
	Cert      *x509.Certificate
	IPDesc    IPDesc
	Time      uint64
	Signature []byte
}

// Returns the length of the byte representation of this IPCertDesc.
func (ip *IPCertDesc) BytesLen() int {
	// See wrappers.PackIPCert.
	return baseIPCertDescLen + len(ip.Cert.Raw) + len(ip.Signature)
}
