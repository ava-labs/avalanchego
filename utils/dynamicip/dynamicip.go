package dynamicip

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	errOpenDNSNoIP = errors.New("opendns returned no ip")
)

// Resolver resolves our public IP
type Resolver interface {
	// Resolve our public IP
	Resolve() (string, error)
	// If false, Resolve always returns an error
	IsResolver() bool
}

// NoResolver doesn't resolve our public IP address
type NoResolver struct{}

func (r *NoResolver) IsResolver() bool {
	return false
}

func (r *NoResolver) Resolve() (string, error) {
	return "", errors.New("invalid resolver")
}

// IFConfigResolves resolves our public IP using openDNS
type OpenDNSResolver struct {
	*net.Resolver
}

func NewOpenDNSResolver() *OpenDNSResolver {
	return &OpenDNSResolver{&net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 10 * time.Second,
			}
			return d.DialContext(ctx, "udp", "resolver1.opendns.com:53")
		},
	}}
}

func (r *OpenDNSResolver) IsResolver() bool {
	return true
}

func (r *OpenDNSResolver) Resolve() (string, error) {
	ip, err := r.Resolver.LookupHost(context.Background(), "myip.opendns.com")
	if err != nil {
		return "", err
	}
	if len(ip) == 0 {
		return "", errOpenDNSNoIP
	}
	return ip[0], nil
}

// IFConfigResolves resolves our public IP using website ifconfig.co
type IFConfigResolver struct{}

func (r *IFConfigResolver) IsResolver() bool {
	return true
}

func (r *IFConfigResolver) Resolve() (string, error) {
	url := "http://ifconfig.co"
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	ip, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response from ifconfig: %w", err)
	}
	ipstr := string(ip)
	ipstr = strings.Replace(ipstr, "\r\n", "", -1)
	ipstr = strings.Replace(ipstr, "\r", "", -1)
	ipstr = strings.Replace(ipstr, "\n", "", -1)
	return ipstr, nil
}

func NewResolver(opt string) Resolver {
	switch opt {
	case "opendns":
		return NewOpenDNSResolver()
	case "ifconfig":
		return &IFConfigResolver{}
	default:
		return &NoResolver{}
	}
}

func FetchExternalIP(resolver Resolver) (string, error) {
	return resolver.Resolve()
}

type DynamicIPManager interface {
	Stop()
}

type NoDynamicIP struct{}

func (noDynamicIP *NoDynamicIP) Stop() {}

// Returns a new dynamic IP that resolves and updates [ip] to our public IP every [updateTimeout].
// Uses [dynamicResolver] to resolve our public ip.
// Stops updating when Stop() is called.
func NewDynamicIPManager(resolver Resolver, updateTimeout time.Duration, log logging.Logger, ip *utils.DynamicIPDesc) DynamicIPManager {
	if resolver.IsResolver() {
		updater := &DynamicIP{
			DynamicIPDesc: ip,
			tickerCloser:  make(chan struct{}),
			log:           log,
			updateTimeout: updateTimeout,
			resolver:      resolver}
		go updater.UpdateExternalIP()
		return updater
	}
	return &NoDynamicIP{}
}

// DynamicIP is an IP address that gets periodically updated to our public IP
type DynamicIP struct {
	*utils.DynamicIPDesc
	tickerCloser  chan struct{}
	log           logging.Logger
	updateTimeout time.Duration
	resolver      Resolver
}

func (dynamicIP *DynamicIP) Stop() {
	close(dynamicIP.tickerCloser)
}

// Update our public IP address in a loop
func (dynamicIP *DynamicIP) UpdateExternalIP() {
	timer := time.NewTimer(dynamicIP.updateTimeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			dynamicIP.update(dynamicIP.resolver)
			timer.Reset(dynamicIP.updateTimeout)
		case <-dynamicIP.tickerCloser:
			return
		}
	}
}

// Fetch and update our public IP address
func (dynamicIP *DynamicIP) update(resolver Resolver) {
	ipstr, err := FetchExternalIP(resolver)
	if err != nil {
		dynamicIP.log.Warn("Fetch external IP failed %s", err)
		return
	}
	newIp := net.ParseIP(ipstr)
	if newIp == nil {
		dynamicIP.log.Warn("Fetched external IP failed to parse %s", ipstr)
		return
	}
	oldIp := dynamicIP.Ip().IP
	dynamicIP.UpdateIP(newIp)
	if !oldIp.Equal(newIp) {
		dynamicIP.log.Info("ExternalIP updated to %s", newIp)
	}
}
