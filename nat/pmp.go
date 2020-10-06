// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/jackpal/gateway"

	natpmp "github.com/jackpal/go-nat-pmp"
)

var (
	pmpClientTimeout = 500 * time.Millisecond
)

// pmpRouter adapts the NAT-PMP protocol implementation so it conforms to the
// common interface.
type pmpRouter struct {
	client *natpmp.Client
}

func (r *pmpRouter) IsNATTraversal() bool {
	return true
}

func (r *pmpRouter) MapPort(
	networkProtocol string,
	newInternalPort uint16,
	newExternalPort uint16,
	mappingName string,
	mappingDuration time.Duration,
) error {
	protocol := string(networkProtocol)
	internalPort := int(newInternalPort)
	externalPort := int(newExternalPort)

	// go-nat-pmp uses seconds to denote their lifetime
	lifetime := mappingDuration.Seconds()
	// Assumes the architecture is at least 32-bit
	if lifetime < 0 || lifetime > math.MaxInt32 {
		return fmt.Errorf("invalid mapping duration range")
	}

	_, err := r.client.AddPortMapping(protocol, internalPort, externalPort, int(lifetime))
	return err
}

func (r *pmpRouter) UnmapPort(
	networkProtocol string,
	internalPort uint16,
	_ uint16) error {
	protocol := string(networkProtocol)
	internalPortInt := int(internalPort)

	_, err := r.client.AddPortMapping(protocol, internalPortInt, 0, 0)
	return err
}

func (r *pmpRouter) ExternalIP() (net.IP, error) {
	response, err := r.client.GetExternalAddress()
	if err != nil {
		return nil, err
	}
	return response.ExternalIPAddress[:], nil
}

func getPMPRouter() *pmpRouter {
	gatewayIP, err := gateway.DiscoverGateway()
	if err != nil {
		return nil
	}

	pmp := &pmpRouter{natpmp.NewClientWithTimeout(gatewayIP, pmpClientTimeout)}
	if _, err := pmp.ExternalIP(); err != nil {
		return nil
	}

	return pmp
}
