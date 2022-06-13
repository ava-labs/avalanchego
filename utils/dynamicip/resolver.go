// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"fmt"
	"net"
)

const (
	ifConfigCoURL = "http://ifconfig.co"
	ifConfigMeURL = "http://ifconfig.me"

	// TODO remove either ifConfig or ifConfigCo.
	// They do the same thing.
	OpenDNS    ResolverName = "opendns"
	IFConfig   ResolverName = "ifconfig"
	IFConfigCo ResolverName = "ifconfigCo"
	IFConfigMe ResolverName = "ifconfigMe"
)

type ResolverName string

// Resolver resolves our public IP
type Resolver interface {
	// Resolve and return our public IP.
	Resolve() (net.IP, error)
}

// Returns a new Resolver that uses the given service
// to resolve our public IP.
// If [resolverService] isn't one of the above,
// returns an error
func NewResolver(resolverName ResolverName) (Resolver, error) {
	switch resolverName {
	case OpenDNS:
		return newOpenDNSResolver(), nil
	case IFConfig, IFConfigCo:
		return &ifConfigResolver{url: ifConfigCoURL}, nil
	case IFConfigMe:
		return &ifConfigResolver{url: ifConfigMeURL}, nil
	default:
		return nil, fmt.Errorf("got unknown resolver: %s", resolverName)
	}
}
