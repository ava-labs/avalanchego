// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const nullStr = "null"

var (
	errMissingQuotes = errors.New("first and last characters should be quotes")
	errBadIP         = errors.New("bad ip format")
)

type IPDesc IPPort

func (ipDesc IPDesc) String() string {
	return IPPort(ipDesc).String()
}

func (ipDesc IPDesc) MarshalJSON() ([]byte, error) {
	return []byte(`"` + ipDesc.String() + `"`), nil
}

func (ipDesc *IPDesc) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	ipPort, err := ToIPPort(str[1:lastIndex])
	if err != nil {
		return fmt.Errorf("couldn't decode to IPPort: %w", err)
	}
	*ipDesc = IPDesc(ipPort)

	return nil
}

// An IP and a port.
type IPPort struct {
	IP   net.IP `json:"ip"`
	Port uint16 `json:"port"`
}

func (ipPort IPPort) Equal(other IPPort) bool {
	return ipPort.Port == other.Port && ipPort.IP.Equal(other.IP)
}

func (ipPort IPPort) String() string {
	return net.JoinHostPort(ipPort.IP.String(), fmt.Sprintf("%d", ipPort.Port))
}

// IsZero returns if the IP or port is zeroed out
func (ipPort IPPort) IsZero() bool {
	ip := ipPort.IP
	return ipPort.Port == 0 ||
		len(ip) == 0 ||
		ip.Equal(net.IPv4zero) ||
		ip.Equal(net.IPv6zero)
}

func ToIPPort(str string) (IPPort, error) {
	host, portStr, err := net.SplitHostPort(str)
	if err != nil {
		return IPPort{}, errBadIP
	}
	port, err := strconv.ParseUint(portStr, 10 /*=base*/, 16 /*=size*/)
	if err != nil {
		// TODO: Should this return a locally defined error? (e.g. errBadPort)
		return IPPort{}, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return IPPort{}, errBadIP
	}
	return IPPort{
		IP:   ip,
		Port: uint16(port),
	}, nil
}

// PackIP packs an ip port pair to the byte array
func PackIP(p *wrappers.Packer, ip IPPort) {
	p.PackFixedBytes(ip.IP.To16())
	p.PackShort(ip.Port)
}
