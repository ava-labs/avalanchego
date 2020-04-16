// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package nat performs network address translation and provides helpers for
// routing  ports.
package nat

import (
	"net"
	"time"
)

// NetworkProtocol is a protocol that will be used through a port
type NetworkProtocol string

// Available protocol
const (
	TCP NetworkProtocol = "TCP"
	UDP NetworkProtocol = "UDP"
)

// Router provides a standard NAT router functions. Specifically, allowing the
// fetching of public IPs and port forwarding to this computer.
type Router interface {
	// mapPort creates a mapping between a port on the local computer to an
	// external port on the router.
	//
	// The mappingName is something displayed on the router, so it is included
	// for completeness.
	MapPort(
		networkProtocol NetworkProtocol,
		newInternalPort uint16,
		newExternalPort uint16,
		mappingName string,
		mappingDuration time.Duration) error

	// UnmapPort clears a mapping that was previous made by a call to MapPort
	UnmapPort(
		networkProtocol NetworkProtocol,
		internalPort uint16,
		externalPort uint16) error

	// Returns the routers IP address on the network the router considers
	// external
	IP() (net.IP, error)
}

// NewRouter returns a new router discovered on the local network
func NewRouter() Router {
	routers := make(chan Router)
	// Because getting a router can take a noticeable amount of time to error,
	// we run these requests in parallel
	go func() {
		routers <- getUPnPRouter()
	}()
	go func() {
		routers <- getPMPRouter()
	}()
	for i := 0; i < 2; i++ {
		if router := <-routers; router != nil {
			return router
		}
	}
	return noRouter{}
}
