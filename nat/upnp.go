// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

const (
	soapTimeout = time.Second
)

var (
	errNoGateway = errors.New("Failed to connect to any avaliable gateways")
)

// upnpClient is the interface used by goupnp for their client implementations
type upnpClient interface {
	// attempts to map connection using the provided protocol from the external
	// port to the internal port for the lease duration.
	AddPortMapping(
		newRemoteHost string,
		newExternalPort uint16,
		newProtocol string,
		newInternalPort uint16,
		newInternalClient string,
		newEnabled bool,
		newPortMappingDescription string,
		newLeaseDuration uint32) error

	// attempt to remove any mapping from the external port.
	DeletePortMapping(
		newRemoteHost string,
		newExternalPort uint16,
		newProtocol string) error

	// attempts to return the external IP address, formatted as a string.
	GetExternalIPAddress() (ip string, err error)

	// returns if there is rsip available, nat enabled, or an unexpected error.
	GetNATRSIPStatus() (newRSIPAvailable bool, natEnabled bool, err error)
}

type upnpRouter struct {
	root   *goupnp.RootDevice
	client upnpClient
}

func (n *upnpRouter) MapPort(
	networkProtocol NetworkProtocol,
	newInternalPort uint16,
	newExternalPort uint16,
	mappingName string,
	mappingDuration time.Duration,
) error {
	ip, err := n.localAddress()
	if err != nil {
		return err
	}

	protocol := string(networkProtocol)
	// goupnp uses seconds to denote their lifetime
	lifetime := uint32(mappingDuration / time.Second)

	// UnmapPort's error is intentionally dropped, because the mapping may not
	// exist.
	n.UnmapPort(networkProtocol, newInternalPort, newExternalPort)

	return n.client.AddPortMapping(
		"", // newRemoteHost isn't used to limit the mapping to a host
		newExternalPort,
		protocol,
		newInternalPort,
		ip.String(), // newInternalClient is the client traffic should be sent to
		true,        // newEnabled enables port mappings
		mappingName,
		lifetime,
	)
}

func (n *upnpRouter) UnmapPort(networkProtocol NetworkProtocol, _, externalPort uint16) error {
	protocol := string(networkProtocol)
	return n.client.DeletePortMapping(
		"", // newRemoteHost isn't used to limit the mapping to a host
		externalPort,
		protocol)
}

func (n *upnpRouter) IP() (net.IP, error) {
	ipStr, err := n.client.GetExternalIPAddress()
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %s", ipStr)
	}
	return ip, nil
}

func (n *upnpRouter) localAddress() (net.IP, error) {
	// attempt to get an address on the router
	deviceAddr, err := net.ResolveUDPAddr("udp4", n.root.URLBase.Host)
	if err != nil {
		return nil, err
	}
	deviceIP := deviceAddr.IP

	netInterfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	// attempt to find one of my ips that the router would know about
	for _, netInterface := range netInterfaces {
		addrs, err := netInterface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			// this is pretty janky, but it seems to be the best way to get the
			// ip mask and properly check if the ip references the device we are
			// connected to
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			if ipNet.Contains(deviceIP) {
				return ipNet.IP, nil
			}
		}
	}
	return nil, fmt.Errorf("couldn't find the local address in the same network as %s", deviceIP)
}

// getUPnPRouter searches for all Gateway Devices that have avaliable
// connections in the goupnp library and returns the first connection it can
// find.
func getUPnPRouter() Router {
	routers := make(chan *upnpRouter)
	// Because DiscoverDevices takes a noticeable amount of time to error, we
	// run these requests in parallel
	go func() {
		routers <- connectToGateway(internetgateway1.URN_WANConnectionDevice_1, gateway1)
	}()
	go func() {
		routers <- connectToGateway(internetgateway2.URN_WANConnectionDevice_2, gateway2)
	}()
	for i := 0; i < 2; i++ {
		if router := <-routers; router != nil {
			return router
		}
	}
	return nil
}

func gateway1(client goupnp.ServiceClient) upnpClient {
	switch client.Service.ServiceType {
	case internetgateway1.URN_WANIPConnection_1:
		return &internetgateway1.WANIPConnection1{ServiceClient: client}
	case internetgateway1.URN_WANPPPConnection_1:
		return &internetgateway1.WANPPPConnection1{ServiceClient: client}
	default:
		return nil
	}
}

func gateway2(client goupnp.ServiceClient) upnpClient {
	switch client.Service.ServiceType {
	case internetgateway2.URN_WANIPConnection_1:
		return &internetgateway2.WANIPConnection1{ServiceClient: client}
	case internetgateway2.URN_WANIPConnection_2:
		return &internetgateway2.WANIPConnection2{ServiceClient: client}
	case internetgateway2.URN_WANPPPConnection_1:
		return &internetgateway2.WANPPPConnection1{ServiceClient: client}
	default:
		return nil
	}
}

func connectToGateway(deviceType string, toClient func(goupnp.ServiceClient) upnpClient) *upnpRouter {
	devs, err := goupnp.DiscoverDevices(deviceType)
	if err != nil {
		return nil
	}
	// we are iterating over all the network devices, acting a possible roots
	for i := range devs {
		dev := &devs[i]
		if dev.Root == nil {
			continue
		}

		// the root device may be a router, so attempt to connect to that
		rootDevice := &dev.Root.Device
		if upnp := getRouter(dev, rootDevice, toClient); upnp != nil {
			return upnp
		}

		// attempt to connect to any sub devices
		devices := rootDevice.Devices
		for i := range devices {
			if upnp := getRouter(dev, &devices[i], toClient); upnp != nil {
				return upnp
			}
		}
	}
	return nil
}

func getRouter(rootDevice *goupnp.MaybeRootDevice, device *goupnp.Device, toClient func(goupnp.ServiceClient) upnpClient) *upnpRouter {
	for i := range device.Services {
		service := &device.Services[i]

		soapClient := service.NewSOAPClient()
		// make sure the client times out if needed
		soapClient.HTTPClient.Timeout = soapTimeout

		// attempt to create a client connection
		serviceClient := goupnp.ServiceClient{
			SOAPClient: soapClient,
			RootDevice: rootDevice.Root,
			Location:   rootDevice.Location,
			Service:    service,
		}
		client := toClient(serviceClient)
		if client == nil {
			continue
		}

		// check whether port mapping is enabled
		if _, nat, err := client.GetNATRSIPStatus(); err != nil || !nat {
			continue
		}

		// we found a router!
		return &upnpRouter{
			root:   rootDevice.Root,
			client: client,
		}
	}
	return nil
}
