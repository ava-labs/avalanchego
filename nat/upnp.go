// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
	"github.com/huin/goupnp/soap"

	"github.com/ava-labs/avalanchego/utils/ips"
)

const (
	// upnpProtocol is intentionally uppercase and should not be confused with
	// pmpProtocol.
	// See:
	// - https://github.com/huin/goupnp/blob/v1.0.3/dcps/internetgateway1/internetgateway1.go#L2361
	// - https://github.com/huin/goupnp/blob/v1.0.3/dcps/internetgateway1/internetgateway1.go#L3618
	// - https://github.com/huin/goupnp/blob/v1.0.3/dcps/internetgateway2/internetgateway2.go#L3919
	upnpProtocol            = "TCP"
	soapRequestTimeout      = 10 * time.Second
	upnpRediscoveryInterval = 30 * time.Second
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
	// Protect the device/client pair shared by all mappings and IP lookups.
	lock           sync.Mutex
	dev            *goupnp.RootDevice
	client         upnpClient
	discoverRouter func() *upnpRouter
	nextDiscovery  time.Time
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
	r.lock.Lock()
	defer r.lock.Unlock()

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
	r.lock.Lock()
	defer r.lock.Unlock()

	lifetime := duration.Seconds()
	if lifetime < 0 || lifetime > math.MaxUint32 {
		return errInvalidLifetime
	}
	return r.mapPort(intPort, extPort, desc, uint32(lifetime))
}

func (r *upnpRouter) mapPort(intPort, extPort uint16, desc string, lifetime uint32) error {
	ip, err := r.localIP()
	if err != nil {
		return err
	}
	err = r.client.AddPortMapping("", extPort, upnpProtocol, intPort,
		ip.String(), true, desc, lifetime)
	// A SOAP fault means the service is reachable. Other failures may indicate
	// a stale control URL. goupnp does not preserve wrapped transport errors.
	var fault *soap.SOAPFaultError
	if err == nil || errors.As(err, &fault) || time.Now().Before(r.nextDiscovery) {
		return err
	}
	// Share the cooldown across mappings, including unsuccessful discoveries.
	// Keep the old client if no replacement is found so transient failures can
	// still recover through the mapper's existing retries.
	r.nextDiscovery = time.Now().Add(upnpRediscoveryInterval)
	replacement := r.discoverRouter()
	if replacement == nil {
		return err
	}
	r.dev = replacement.dev
	r.client = replacement.client

	// The new gateway may use a different local interface as well as endpoint.
	ip, err = r.localIP()
	if err != nil {
		return err
	}
	return r.client.AddPortMapping("", extPort, upnpProtocol, intPort,
		ip.String(), true, desc, lifetime)
}

func (r *upnpRouter) UnmapPort(_, extPort uint16) error {
	r.lock.Lock()
	defer r.lock.Unlock()

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
func discover(target string, discoverDevices func(string) ([]goupnp.MaybeRootDevice, error)) *upnpRouter {
	devs, err := discoverDevices(target)
	if err != nil {
		return nil
	}

	// Each device must publish a result, even when its description is invalid.
	// Buffer all results so returning the first router cannot strand senders.
	router := make(chan *upnpRouter, len(devs))
	for i := 0; i < len(devs); i++ {
		if devs[i].Root == nil {
			router <- nil
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
	return getUPnPRouterWithDiscover(func(target string) ([]goupnp.MaybeRootDevice, error) {
		// Bound description fetches as well as the SSDP search. Rediscovery is
		// called while mappings and IP lookups wait for the shared router.
		ctx, cancel := context.WithTimeout(context.Background(), soapRequestTimeout)
		defer cancel()
		return goupnp.DiscoverDevicesCtx(ctx, target)
	})
}

func getUPnPRouterWithDiscover(discoverDevices func(string) ([]goupnp.MaybeRootDevice, error)) *upnpRouter {
	targets := []string{
		internetgateway1.URN_WANConnectionDevice_1,
		internetgateway2.URN_WANConnectionDevice_2,
	}

	routers := make(chan *upnpRouter, len(targets))

	for _, urn := range targets {
		go func(urn string) {
			routers <- discover(urn, discoverDevices)
		}(urn)
	}

	for i := 0; i < len(targets); i++ {
		if r := <-routers; r != nil {
			r.discoverRouter = func() *upnpRouter {
				return getUPnPRouterWithDiscover(discoverDevices)
			}
			return r
		}
	}

	return nil
}
