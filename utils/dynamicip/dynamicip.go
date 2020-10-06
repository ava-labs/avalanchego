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

type DynamicResolver interface {
	Resolve() (string, error)
	IsResolver() bool
}

type NoResolver struct {
}

func (r *NoResolver) IsResolver() bool {
	return false
}

func (r *NoResolver) Resolve() (string, error) {
	return "", errors.New("invalid resolver")
}

type OpenDNSResolver struct {
	*net.Resolver
}

func NewOpenDNSResolver() *OpenDNSResolver {
	return &OpenDNSResolver{&net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(10000),
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
		return "", errors.New(fmt.Sprintf("opendns returned no ip"))
	}
	return ip[0], nil
}

type IFConfigResolver struct {
}

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
		return "", err
	}
	ipstr := string(ip)
	ipstr = strings.Replace(ipstr, "\r\n", "", -1)
	ipstr = strings.Replace(ipstr, "\r", "", -1)
	ipstr = strings.Replace(ipstr, "\n", "", -1)
	return ipstr, nil
}

func NewDynamicResolver(opt string) DynamicResolver {
	switch opt {
	case "opendns":
		return NewOpenDNSResolver()
	case "ifconfig":
		return &IFConfigResolver{}
	default:
		return &NoResolver{}
	}
}

func FetchExternalIP(dynamicResolver DynamicResolver) (string, error) {
	return dynamicResolver.Resolve()
}

type DynamicIPManager interface {
	Stop()
}

type NoDynamicIP struct {
}

func (noDynamicIP *NoDynamicIP) Stop() {
}

type DynamicIP struct {
	tickerCloser    chan struct{}
	log             logging.Logger
	ip              *utils.DynamicIPDesc
	updateTimeout   time.Duration
	dynamicResolver DynamicResolver
}

func NewDynamicIPManager(dynamicResolver DynamicResolver, updateTimeout time.Duration, log logging.Logger, ip *utils.DynamicIPDesc) DynamicIPManager {
	if dynamicResolver.IsResolver() {
		updater := &DynamicIP{tickerCloser: make(chan struct{}), log: log, ip: ip, updateTimeout: updateTimeout, dynamicResolver: dynamicResolver}
		go updater.UpdateExternalIP()
		return updater
	}
	return &NoDynamicIP{}
}

func (dynamicIP *DynamicIP) Stop() {
	close(dynamicIP.tickerCloser)
}

func (dynamicIP *DynamicIP) UpdateExternalIP() {
	timer := time.NewTimer(dynamicIP.updateTimeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			dynamicIP.updateIP(dynamicIP.dynamicResolver)
			timer.Reset(dynamicIP.updateTimeout)
		case <-dynamicIP.tickerCloser:
			return
		}
	}
}

func (dynamicIP *DynamicIP) updateIP(dynamicResolver DynamicResolver) {
	ipstr, err := FetchExternalIP(dynamicResolver)
	if err != nil {
		dynamicIP.log.Warn("Fetch external IP failed %s", err)
		return
	}
	newIp := net.ParseIP(ipstr)
	if newIp == nil {
		dynamicIP.log.Warn("Fetched external IP failed to parse %s", ipstr)
		return
	}
	oldIp := dynamicIP.ip.Ip().IP
	dynamicIP.ip.UpdateIP(newIp)
	if !oldIp.Equal(newIp) {
		dynamicIP.log.Info("ExternalIP updated to %s", newIp)
	}
}
