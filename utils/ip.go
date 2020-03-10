// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
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
	return fmt.Sprintf("%s%s", ipDesc.IP, ipDesc.PortString())
}

// ToIPDesc ...
// TODO: this was kinda hacked together, it should be verified.
func ToIPDesc(str string) (IPDesc, error) {
	parts := strings.Split(str, ":")
	if len(parts) != 2 {
		return IPDesc{}, errBadIP
	}
	port, err := strconv.ParseUint(parts[1], 10 /*=base*/, 16 /*=size*/)
	if err != nil {
		return IPDesc{}, err
	}
	ip := net.ParseIP(parts[0])
	if ip == nil {
		return IPDesc{}, errBadIP
	}
	return IPDesc{
		IP:   ip,
		Port: uint16(port),
	}, nil
}

// MyIP ...
func MyIP() net.IP {
	// TODO: Change this to consult a json-returning external service
	return net.ParseIP("127.0.0.1")
}
