// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/soap"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestUPnPRouterControlPortChange(t *testing.T) {
	require := require.New(t)
	oldServer, router, oldRequests := newTestUPnPRouter(t)
	// Start the replacement before closing the old listener so the ports differ.
	_, replacement, newRequests := newTestUPnPRouter(t)
	require.NotEqual(router.dev.URLBase.Host, replacement.dev.URLBase.Host)

	discoveries := 0
	router.discoverRouter = func() *upnpRouter {
		discoveries++
		return replacement
	}

	mapper := NewPortMapper(logging.NoLog{}, router)
	mapper.Map(9651, 19651, "test mapping", nil, time.Hour)
	closeMapper := sync.OnceFunc(mapper.UnmapAllPorts)
	t.Cleanup(closeMapper)
	require.Len(oldRequests.snapshot(), 1)
	oldServer.Close()

	// Exercise the existing renewal retry path with the same shared Router.
	require.NoError(mapper.retryMapPort(9651, 19651, "test mapping", mapTimeout))
	require.Equal(1, discoveries)
	require.Same(router, mapper.r)
	require.NoError(router.MapPort(9652, 19652, "second mapping", time.Minute))

	ip := utils.NewAtomic(netip.MustParseAddrPort("192.0.2.1:19651"))
	mapper.updateIP(ip)
	require.Equal(netip.MustParseAddrPort("203.0.113.1:19651"), ip.Get())
	require.NoError(router.UnmapPort(9652, 19652))

	requests := newRequests.snapshot()
	require.Len(requests, 4)
	require.Equal(upnpTestRequest{
		Action:         "AddPortMapping",
		ExternalPort:   19651,
		Protocol:       upnpProtocol,
		InternalPort:   9651,
		InternalClient: "127.0.0.1",
		Enabled:        true,
		Description:    "test mapping",
		LeaseDuration:  uint32(mapTimeout.Seconds()),
	}, requests[0])
	require.Equal("GetExternalIPAddress", requests[2].Action)
	require.Equal("DeletePortMapping", requests[3].Action)
	require.Equal(uint16(19652), requests[3].ExternalPort)
	closeMapper()
	requests = newRequests.snapshot()
	require.Len(requests, 5)
	require.Equal("DeletePortMapping", requests[4].Action)
	require.Equal(uint16(19651), requests[4].ExternalPort)
}

func TestUPnPRouterRediscoveryCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		require := require.New(t)
		mapErr := errors.New("control endpoint unavailable")
		client := &upnpTestClient{mapErr: mapErr}
		router := newTestUPnPRouterWithClient(client)
		replacementClient := &upnpTestClient{}
		replacement := newTestUPnPRouterWithClient(replacementClient)
		discoveries := 0
		router.discoverRouter = func() *upnpRouter {
			discoveries++
			if discoveries == 1 {
				return nil
			}
			return replacement
		}

		mapper := NewPortMapper(logging.NoLog{}, router)
		require.ErrorIs(mapper.retryMapPort(1, 2, "test", time.Minute), mapErr)
		require.Equal(maxRefreshRetries, client.mapCalls)
		require.Equal(1, discoveries)
		require.Same(client, router.client)

		// The mapper slept three seconds. Calls for other ports share the same
		// cooldown, and discovery becomes eligible exactly at the boundary.
		time.Sleep(upnpRediscoveryInterval - 3*time.Second - time.Nanosecond)
		require.ErrorIs(router.MapPort(3, 4, "test", time.Minute), mapErr)
		require.Equal(1, discoveries)
		time.Sleep(time.Nanosecond)
		require.NoError(router.MapPort(3, 4, "test", time.Minute))
		require.Equal(2, discoveries)
		require.Equal(1, replacementClient.mapCalls)
		require.Same(replacement.dev, router.dev)
		require.Same(replacementClient, router.client)
	})
}

func TestUPnPRouterFailedRediscoveryRetry(t *testing.T) {
	require := require.New(t)
	oldErr := errors.New("old endpoint unavailable")
	newErr := errors.New("new endpoint unavailable")
	client := &upnpTestClient{mapErr: oldErr}
	router := newTestUPnPRouterWithClient(client)
	replacementClient := &upnpTestClient{mapErr: newErr}
	discoveries := 0
	router.discoverRouter = func() *upnpRouter {
		discoveries++
		return newTestUPnPRouterWithClient(replacementClient)
	}
	require.ErrorIs(router.MapPort(1, 2, "test", time.Minute), newErr)
	require.Equal(1, client.mapCalls)
	require.Equal(1, replacementClient.mapCalls)
	require.Equal(1, discoveries)
	// A failed immediate retry must not recursively rediscover the router.
	require.ErrorIs(router.MapPort(1, 2, "test", time.Minute), newErr)
	require.Equal(2, replacementClient.mapCalls)
	require.Equal(1, discoveries)
}

func TestUPnPRouterDoesNotRediscover(t *testing.T) {
	fault := &soap.SOAPFaultError{FaultCode: "s:Client", FaultString: "ConflictInMappingEntry"}
	for _, test := range []struct {
		name          string
		mapErr        error
		duration      time.Duration
		expectedErr   error
		expectedCalls int
	}{
		{name: "success", duration: time.Minute, expectedCalls: 1},
		{name: "SOAP fault", duration: time.Minute, mapErr: fault, expectedErr: fault, expectedCalls: 1},
		{name: "wrapped SOAP fault", duration: time.Minute, mapErr: fmt.Errorf("wrapped: %w", fault), expectedErr: fault, expectedCalls: 1},
		{name: "negative lifetime", duration: -time.Second, expectedErr: errInvalidLifetime},
		{name: "overflowing lifetime", duration: time.Duration(1<<32) * time.Second, expectedErr: errInvalidLifetime},
	} {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			client := &upnpTestClient{mapErr: test.mapErr}
			router := newTestUPnPRouterWithClient(client)
			router.discoverRouter = func() *upnpRouter {
				t.Error("unexpected rediscovery")
				return nil
			}
			err := router.MapPort(1, 2, "test", test.duration)
			if test.expectedErr == nil {
				require.NoError(err)
			} else {
				require.ErrorIs(err, test.expectedErr)
			}
			require.Equal(test.expectedCalls, client.mapCalls)
		})
	}
}

func TestUPnPRouterConcurrentRediscovery(t *testing.T) {
	require := require.New(t)
	oldServer, router, _ := newTestUPnPRouter(t)
	_, replacement, _ := newTestUPnPRouter(t)
	oldServer.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	discoveries := 0
	router.discoverRouter = func() *upnpRouter {
		discoveries++
		close(started)
		<-release
		return replacement
	}
	const numCalls = 12
	results := make(chan error, numCalls+1)
	go func() { results <- router.MapPort(1, 2, "test", time.Minute) }()
	<-started
	for i := range numCalls {
		go func() {
			switch i % 3 {
			case 0:
				results <- router.MapPort(3, 4, "test", time.Minute)
			case 1:
				_, err := router.ExternalIP()
				results <- err
			case 2:
				results <- router.UnmapPort(5, 6)
			}
		}()
	}
	close(release)
	for range numCalls + 1 {
		require.NoError(<-results)
	}
	require.Equal(1, discoveries)
}

func TestUPnPDiscovery(t *testing.T) {
	for _, test := range []string{"discovery error", "no devices", "invalid description", "multiple devices"} {
		t.Run(test, func(t *testing.T) {
			// Registered first so server cleanup runs before the leak check.
			ignoreCurrent := goleak.IgnoreCurrent()
			t.Cleanup(func() { goleak.VerifyNone(t, ignoreCurrent) })
			_, router, _ := newTestUPnPRouter(t)
			discoverDevices := func(string) ([]goupnp.MaybeRootDevice, error) {
				switch test {
				case "discovery error":
					return nil, errors.New("discovery failed")
				case "no devices":
					return nil, nil
				case "invalid description":
					return []goupnp.MaybeRootDevice{{Err: errors.New("invalid description")}}, nil
				default:
					return []goupnp.MaybeRootDevice{
						{},
						{Root: router.dev, Location: &router.dev.URLBase},
						{Root: router.dev, Location: &router.dev.URLBase},
					}, nil
				}
			}
			found := getUPnPRouterWithDiscover(discoverDevices)
			if test == "multiple devices" {
				require.NotNil(t, found)
				require.NotNil(t, found.discoverRouter)
			} else {
				require.Nil(t, found)
			}
		})
	}
}

type upnpTestClient struct {
	upnpClient
	mapErr   error
	mapCalls int
}

func (c *upnpTestClient) AddPortMapping(string, uint16, string, uint16, string, bool, string, uint32) error {
	c.mapCalls++
	return c.mapErr
}

func newTestUPnPRouterWithClient(client upnpClient) *upnpRouter {
	return &upnpRouter{
		dev:    &goupnp.RootDevice{URLBase: url.URL{Host: "127.0.0.1:1"}},
		client: client,
	}
}

type upnpTestRequest struct {
	Action         string `xml:"-"`
	RemoteHost     string `xml:"NewRemoteHost"`
	ExternalPort   uint16 `xml:"NewExternalPort"`
	Protocol       string `xml:"NewProtocol"`
	InternalPort   uint16 `xml:"NewInternalPort"`
	InternalClient string `xml:"NewInternalClient"`
	Enabled        bool   `xml:"NewEnabled"`
	Description    string `xml:"NewPortMappingDescription"`
	LeaseDuration  uint32 `xml:"NewLeaseDuration"`
}

type upnpTestRequests struct {
	lock     sync.Mutex
	requests []upnpTestRequest
}

func (r *upnpTestRequests) snapshot() []upnpTestRequest {
	r.lock.Lock()
	defer r.lock.Unlock()
	return append([]upnpTestRequest(nil), r.requests...)
}

// Only loopback SOAP traffic is used. Discovery is always injected by tests;
// no SSDP broadcasts, real gateways, or outbound IP probes are needed.
func newTestUPnPRouter(t *testing.T) (*httptest.Server, *upnpRouter, *upnpTestRequests) {
	t.Helper()
	require := require.New(t)
	requests := &upnpTestRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := strings.TrimSuffix(strings.SplitN(r.Header.Get("Soapaction"), "#", 2)[1], `"`)
		var envelope struct {
			Body struct {
				Request upnpTestRequest `xml:",any"`
			} `xml:"Body"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decoding SOAP request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		envelope.Body.Request.Action = action
		requests.lock.Lock()
		requests.requests = append(requests.requests, envelope.Body.Request)
		requests.lock.Unlock()

		var result string
		switch action {
		case "GetExternalIPAddress":
			result = "<NewExternalIPAddress>203.0.113.1</NewExternalIPAddress>"
		case "GetNATRSIPStatus":
			result = "<NewRSIPAvailable>0</NewRSIPAvailable><NewNATEnabled>1</NewNATEnabled>"
		}
		w.Header().Set("Content-Type", "text/xml")
		_, err := io.WriteString(w, fmt.Sprintf(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`, action, internetgateway1.URN_WANIPConnection_1, result, action))
		if err != nil {
			t.Errorf("writing SOAP response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	location, err := url.Parse(server.URL)
	require.NoError(err)
	dev := &goupnp.RootDevice{
		Device: goupnp.Device{
			Services: []goupnp.Service{{
				ServiceType: internetgateway1.URN_WANIPConnection_1,
				ControlURL:  goupnp.URLField{Str: "/control"},
			}},
		},
	}
	dev.SetURLBase(location)
	clients, err := goupnp.NewServiceClientsFromRootDevice(dev, location, internetgateway1.URN_WANIPConnection_1)
	require.NoError(err)
	require.Len(clients, 1)
	clients[0].SOAPClient.HTTPClient.Timeout = soapRequestTimeout
	return server, &upnpRouter{
		dev:    dev,
		client: getUPnPClient(clients[0]),
		discoverRouter: func() *upnpRouter {
			t.Error("unexpected rediscovery")
			return nil
		},
	}, requests
}
