// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net"
	"time"

	"github.com/jackpal/gateway"
	"github.com/jackpal/go-nat-pmp"
)

var (
	pmpClientTimeout = 500 * time.Millisecond
)

// natPMPClient adapts the NAT-PMP protocol implementation so it conforms to
// the common interface.
type pmpClient struct {
	client *natpmp.Client
}

func (pmp *pmpClient) MapPort(
	networkProtocol NetworkProtocol,
	newInternalPort uint16,
	newExternalPort uint16,
	mappingName string,
	mappingDuration time.Duration) error {
	protocol := string(networkProtocol)
	internalPort := int(newInternalPort)
	externalPort := int(newExternalPort)
	// go-nat-pmp uses seconds to denote their lifetime
	lifetime := int(mappingDuration / time.Second)

	_, err := pmp.client.AddPortMapping(protocol, internalPort, externalPort, lifetime)
	return err
}

func (pmp *pmpClient) UnmapPort(
	networkProtocol NetworkProtocol,
	internalPort uint16,
	_ uint16) error {
	protocol := string(networkProtocol)
	internalPortInt := int(internalPort)

	_, err := pmp.client.AddPortMapping(protocol, internalPortInt, 0, 0)
	return err
}

func (pmp *pmpClient) IP() (net.IP, error) {
	response, err := pmp.client.GetExternalAddress()
	if err != nil {
		return nil, err
	}
	return response.ExternalIPAddress[:], nil
}

func getPMPRouter() Router {
	gatewayIP, err := gateway.DiscoverGateway()
	if err != nil {
		return nil
	}

	pmp := &pmpClient{natpmp.NewClientWithTimeout(gatewayIP, pmpClientTimeout)}
	if _, err := pmp.IP(); err != nil {
		return nil
	}

	return pmp
}
