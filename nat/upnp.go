// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"

	"github.com/ava-labs/avalanchego/utils/ips"
)

const (
	// upnpProtocol is intentionally uppercase and should not be confused with
	// pmpProtocol.
	// See:
	// - https://github.com/huin/goupnp/blob/v1.0.3/dcps/internetgateway1/internetgateway1.go#L2361
	// - https://github.com/huin/goupnp/blob/v1.0.3/dcps/internetgateway1/internetgateway1.go#L3618
	// - https://github.com/huin/goupnp/blob/v1.0.3/dcps/internetgateway2/internetgateway2.go#L3919
	upnpProtocol       = "TCP"
	soapRequestTimeout = 10 * time.Second
)

var _ Router = (*upnpRouter)(nil)

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

	// attempts to get port mapping information give a external port and protocol
	GetSpecificPortMappingEntry(
		NewRemoteHost string,
		NewExternalPort uint16,
		NewProtocol string,
	) (
		NewInternalPort uint16,
		NewInternalClient string,
		NewEnabled bool,
		NewPortMappingDescription string,
		NewLeaseDuration uint32,
		err error,
	)
}

type upnpRouter struct {
	dev    *goupnp.RootDevice
	client upnpClient
}

func (*upnpRouter) SupportsNAT() bool {
	return true
}

func (r *upnpRouter) localIP() (net.IP, error) {
	// attempt to get an address on the router
	deviceAddr, err := net.ResolveUDPAddr("udp", r.dev.URLBase.Host)
	if err != nil {
		return nil, err
	}
	deviceIP := deviceAddr.IP

	netInterfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	// attempt to find one of my IPs that matches router's record
	for _, netInterface := range netInterfaces {
		addrs, err := netInterface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
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

func (r *upnpRouter) ExternalIP() (netip.Addr, error) {
	str, err := r.client.GetExternalIPAddress()
	if err != nil {
		return netip.Addr{}, err
	}
	return ips.ParseAddr(str)
}

func (r *upnpRouter) MapPort(
	intPort,
	extPort uint16,
	desc string,
	duration time.Duration,
) error {
	ip, err := r.localIP()
	if err != nil {
		return err
	}
	lifetime := duration.Seconds()
	if lifetime < 0 || lifetime > math.MaxUint32 {
		return errInvalidLifetime
	}

	return r.client.AddPortMapping("", extPort, upnpProtocol, intPort,
		ip.String(), true, desc, uint32(lifetime))
}

func (r *upnpRouter) UnmapPort(_, extPort uint16) error {
	return r.client.DeletePortMapping("", extPort, upnpProtocol)
}

// create UPnP SOAP service client with URN
func getUPnPClient(client goupnp.ServiceClient) upnpClient {
	switch client.Service.ServiceType {
	case internetgateway1.URN_WANIPConnection_1:
		return &internetgateway1.WANIPConnection1{ServiceClient: client}
	case internetgateway1.URN_WANPPPConnection_1:
		return &internetgateway1.WANPPPConnection1{ServiceClient: client}
	case internetgateway2.URN_WANIPConnection_2:
		return &internetgateway2.WANIPConnection2{ServiceClient: client}
	default:
		return nil
	}
}

// discover() tries to find gateway device
func discover(target string) *upnpRouter {
	devs, err := goupnp.DiscoverDevices(target)
	if err != nil {
		return nil
	}

	router := make(chan *upnpRouter)
	for i := 0; i < len(devs); i++ {
		if devs[i].Root == nil {
			continue
		}
		go func(dev *goupnp.MaybeRootDevice) {
			var r *upnpRouter
			dev.Root.Device.VisitServices(func(service *goupnp.Service) {
				c := goupnp.ServiceClient{
					SOAPClient: service.NewSOAPClient(),
					RootDevice: dev.Root,
					Location:   dev.Location,
					Service:    service,
				}
				c.SOAPClient.HTTPClient.Timeout = soapRequestTimeout
				client := getUPnPClient(c)
				if client == nil {
					return
				}
				if _, nat, err := client.GetNATRSIPStatus(); err != nil || !nat {
					return
				}
				newRouter := &upnpRouter{
					dev:    dev.Root,
					client: client,
				}
				if _, err := newRouter.localIP(); err != nil {
					return
				}
				r = newRouter
			})
			router <- r
		}(&devs[i])
	}

	for i := 0; i < len(devs); i++ {
		if r := <-router; r != nil {
			return r
		}
	}

	return nil
}

// getUPnPRouter searches for internet gateway using both Device Control Protocol
// and returns the first one it can find. It returns nil if no UPnP gateway is found
func getUPnPRouter() *upnpRouter {
	targets := []string{
		internetgateway1.URN_WANConnectionDevice_1,
		internetgateway2.URN_WANConnectionDevice_2,
	}

	routers := make(chan *upnpRouter)

	for _, urn := range targets {
		go func(urn string) {
			routers <- discover(urn)
		}(urn)
	}

	for i := 0; i < len(targets); i++ {
		if r := <-routers; r != nil {
			return r
		}
	}

	return nil
}
