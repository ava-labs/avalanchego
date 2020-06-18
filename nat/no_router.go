// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"fmt"
	"net"
	"time"
)

const googleDNSServer = "8.8.8.8:80"

type noRouter struct {
	ip net.IP
}

func (noRouter) MapPort(_ string, intPort, extPort uint16, _ string, _ time.Duration) error {
	if intPort != extPort {
		return fmt.Errorf("cannot map port %d to %d", intPort, extPort)
	}
	return nil
}

func (noRouter) UnmapPort(string, uint16, uint16) error {
	return nil
}

func (r noRouter) ExternalIP() (net.IP, error) {
	return r.ip, nil
}

func (noRouter) GetPortMappingEntry(uint16, string) (string, uint16, string, error) {
	return "", 0, "", nil
}

func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", googleDNSServer)
	if err != nil {
		return nil, err
	}

	if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return udpAddr.IP, conn.Close()
	}

	conn.Close()
	return nil, fmt.Errorf("getting outbound IP failed")
}

// NewNoRouter returns a router that assumes the network is public
func NewNoRouter() Router {
	ip, err := getOutboundIP()
	if err != nil {
		return nil
	}
	return &noRouter{
		ip: ip,
	}
}
