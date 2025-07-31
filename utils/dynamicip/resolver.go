// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const (
	ifConfigCoURL = "http://ifconfig.co"
	ifConfigMeURL = "http://ifconfig.me"
	// Note: All of the names below must be lowercase
	// because we lowercase the user's input in NewResolver.
	// TODO remove either ifConfig or ifConfigCo.
	// They do the same thing.
	OpenDNSName    = "opendns"
	IFConfigName   = "ifconfig"
	IFConfigCoName = "ifconfigco"
	IFConfigMeName = "ifconfigme"
)

var errUnknownResolver = errors.New("unknown resolver")

// Resolver resolves our public IP
type Resolver interface {
	// Resolve and return our public IP.
	Resolve(context.Context) (netip.Addr, error)
}

// Returns a new Resolver that uses the given service
// to resolve our public IP.
// [resolverName] must be one of:
// [OpenDNSName], [IFConfigName], [IFConfigCoName], [IFConfigMeName].
// If [resolverService] isn't one of the above, returns an error
func NewResolver(resolverName string) (Resolver, error) {
	switch strings.ToLower(resolverName) {
	case OpenDNSName:
		return newOpenDNSResolver(), nil
	case IFConfigName, IFConfigCoName:
		return &ifConfigResolver{url: ifConfigCoURL}, nil
	case IFConfigMeName:
		return &ifConfigResolver{url: ifConfigMeURL}, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownResolver, resolverName)
	}
}
