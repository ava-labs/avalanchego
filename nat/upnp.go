package nat

import (
	"fmt"
	"net"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

const (
	soapRequestTimeout = 3 * time.Second
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

func (r *upnpRouter) localIP() (net.IP, error) {
	// attempt to get an address on the router
	deviceAddr, err := net.ResolveUDPAddr("udp4", r.dev.URLBase.Host)
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

func (r *upnpRouter) ExternalIP() (net.IP, error) {
	str, err := r.client.GetExternalIPAddress()
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(str)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %s", str)
	}
	return ip, nil
}

func (r *upnpRouter) MapPort(protocol string, intport, extport uint16,
	desc string, duration time.Duration) error {
	ip, err := r.localIP()
	if err != nil {
		return nil
	}
	lifetime := uint32(duration / time.Second)

	return r.client.AddPortMapping("", extport, protocol, intport,
		ip.String(), true, desc, lifetime)
}

func (r *upnpRouter) UnmapPort(protocol string, extport uint16) error {
	return r.client.DeletePortMapping("", extport, protocol)
}

func (r *upnpRouter) IsMapped(extport uint16, protocol string) bool {
	_, _, enabled, _, _, _ := r.client.GetSpecificPortMappingEntry("", extport, protocol)
	return enabled
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

func getUPnPClient(client goupnp.ServiceClient) upnpClient {
	c := gateway1(client)
	if c != nil {
		return c
	}
	return gateway2(client)
}

func getRootDevice(dev *goupnp.MaybeRootDevice) *upnpRouter {
	var router *upnpRouter
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
		router = &upnpRouter{dev.Root, client}
		if router == nil {
			return
		}
		if _, nat, err := router.client.GetNATRSIPStatus(); err != nil || !nat {
			router = nil
			return
		}
	})
	return router
}

func discover(target string) *upnpRouter {
	devs, err := goupnp.DiscoverDevices(target)
	if err != nil {
		return nil
	}
	for i := 0; i < len(devs); i++ {
		if devs[i].Root == nil {
			continue
		}
		u := getRootDevice(&devs[i])
		if u != nil {
			return u
		}
	}
	return gateway2(client)
}

func getUPnPRouter() *upnpRouter {
	targets := []string{
		internetgateway1.URN_WANConnectionDevice_1,
		internetgateway2.URN_WANConnectionDevice_2,
	}

	routers := make(chan *upnpRouter, len(targets))

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

// getUPnPRouter searches for internet gateway using both Device Control Protocol
// and returns the first one it can find. It returns nil if no UPnP gateway is found
func getUPnPRouter() *upnpRouter {
	targets := []string{
		internetgateway1.URN_WANConnectionDevice_1,
		internetgateway2.URN_WANConnectionDevice_2,
	}

	routers := make(chan *upnpRouter, len(targets))

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
