// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
)

// This was taken from: https://stackoverflow.com/a/50825191/3478466
var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addr
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Errorf("parse error on %q: %w", cidr, err))
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

var errBadIP = errors.New("bad ip format")

type IPDesc struct {
	IP   net.IP `json:"ip"`
	Port uint16 `json:"port"`
}

func (ipDesc IPDesc) Equal(otherIPDesc IPDesc) bool {
	return ipDesc.Port == otherIPDesc.Port &&
		ipDesc.IP.Equal(otherIPDesc.IP)
}

func (ipDesc IPDesc) PortString() string {
	return fmt.Sprintf(":%d", ipDesc.Port)
}

func (ipDesc IPDesc) String() string {
	return net.JoinHostPort(ipDesc.IP.String(), fmt.Sprintf("%d", ipDesc.Port))
}

// IsPrivate attempts to decide if the ip address in this descriptor is a local
// ip address.
// This function was taken from: https://stackoverflow.com/a/50825191/3478466
func (ipDesc IPDesc) IsPrivate() bool {
	ip := ipDesc.IP
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// IsZero returns if the IP or port is zeroed out
func (ipDesc IPDesc) IsZero() bool {
	ip := ipDesc.IP
	return ipDesc.Port == 0 ||
		len(ip) == 0 ||
		ip.Equal(net.IPv4zero) ||
		ip.Equal(net.IPv6zero)
}

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

type IPDescContainer struct {
	*IPDesc
	lock sync.RWMutex
}

type DynamicIPDesc struct {
	*IPDescContainer
}

func NewDynamicIPDesc(ip net.IP, port uint16) DynamicIPDesc {
	return DynamicIPDesc{IPDescContainer: &IPDescContainer{IPDesc: &IPDesc{IP: ip, Port: port}}}
}

func (i *DynamicIPDesc) IP() IPDesc {
	var ip IPDesc
	i.lock.RLock()
	ip = *i.IPDesc
	i.lock.RUnlock()
	return ip
}

func (i *DynamicIPDesc) Update(ip IPDesc) {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.IPDesc = &ip
}

func (i *DynamicIPDesc) UpdatePort(port uint16) {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.IPDesc.Port = port
}

func (i *DynamicIPDesc) UpdateIP(ip net.IP) {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.IPDesc.IP = ip
}
