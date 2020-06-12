package nat

import (
	"fmt"
	"net"
	"time"
)

const googleDNSServer = "8.8.8.8:80"

type publicIP struct {
	ip net.IP
}

func (publicIP) MapPort(protocol string, intport, extport uint16, desc string, duration time.Duration) error {
	if intport != extport {
		return fmt.Errorf("cannot map port %d to %d", intport, extport)
	}
	return nil
}

func (publicIP) UnmapPort(protocol string, extport uint16) error {
	return nil
}

func (r publicIP) ExternalIP() (net.IP, error) {
	return r.ip, nil
}

func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", googleDNSServer)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}

func NewPublicIP() *publicIP {
	ip, err := getOutboundIP()
	if err != nil {
		return nil
	}
	return &publicIP{
		ip: ip,
	}
}
