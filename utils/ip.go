// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

var (
	errBadIP = errors.New("bad ip format")
)

// IPDesc ...
type IPDesc struct {
	IP   net.IP
	Port uint16
}

// Equal ...
func (ipDesc IPDesc) Equal(otherIPDesc IPDesc) bool {
	return ipDesc.Port == otherIPDesc.Port &&
		ipDesc.IP.Equal(otherIPDesc.IP)
}

// PortString ...
func (ipDesc IPDesc) PortString() string {
	return fmt.Sprintf(":%d", ipDesc.Port)
}

func (ipDesc IPDesc) String() string {
	return net.JoinHostPort(ipDesc.IP.String(), fmt.Sprintf("%d", ipDesc.Port))
}

// ToIPDesc ...
func ToIPDesc(str string) (IPDesc, error) {
	host, portStr, err := net.SplitHostPort(str)
	if err != nil {
		return IPDesc{}, errBadIP
	}
	port, err := strconv.ParseUint(portStr, 10 /*=base*/, 16 /*=size*/)
	if err != nil {
		// TODO: Should this return a locally defined error? (e.g. errBadPort)
		return IPDesc{}, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return IPDesc{}, errBadIP
	}
	return IPDesc{
		IP:   ip,
		Port: uint16(port),
	}, nil
}
