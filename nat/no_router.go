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

func (noRouter) MapPort(protocol string, intport, extport uint16, desc string, duration time.Duration) error {
	if intport != extport {
		return fmt.Errorf("cannot map port %d to %d", intport, extport)
	}
	return nil
}

func (noRouter) UnmapPort(protocol string, extport uint16) error {
	return nil
}

func (r noRouter) ExternalIP() (net.IP, error) {
	return r.ip, nil
}

func (noRouter) IsMapped(uint16, string) bool {
	return false
}

func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", googleDNSServer)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}

func NewNoRouter() *noRouter {
	ip, err := getOutboundIP()
	if err != nil {
		return nil
	}
	return &noRouter{
		ip: ip,
	}
}
