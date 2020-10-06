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

type ExternalIPUpdaterInterface interface {
	Stop()
}

type NoExternalIPUpdater struct {
}

func (u *NoExternalIPUpdater) Stop() {
}

type ExternalIPUpdater struct {
	tickerCloser    chan struct{}
	log             logging.Logger
	ip              *utils.DynamicIPDesc
	updateTimeout   time.Duration
	dynamicResolver DynamicResolver
}

func NewExternalIPUpdater(dynamicResolver DynamicResolver, updateTimeout time.Duration, log logging.Logger, ip *utils.DynamicIPDesc) ExternalIPUpdaterInterface {
	if dynamicResolver.IsResolver() {
		updater := &ExternalIPUpdater{log: log, ip: ip, updateTimeout: updateTimeout, dynamicResolver: dynamicResolver}
		go updater.UpdateExternalIP()
		return updater
	}
	return &NoExternalIPUpdater{}
}

func (u *ExternalIPUpdater) Stop() {
	close(u.tickerCloser)
}

func (u *ExternalIPUpdater) UpdateExternalIP() {
	timer := time.NewTimer(u.updateTimeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			u.updateIP(u.dynamicResolver)
			timer.Reset(u.updateTimeout)
		case <-u.tickerCloser:
			return
		}
	}
}

func (u *ExternalIPUpdater) updateIP(dynamicResolver DynamicResolver) {
	ipstr, err := FetchExternalIP(dynamicResolver)
	if err != nil {
		u.log.Warn("Fetch external IP failed %s", err)
		return
	}
	newIp := net.ParseIP(ipstr)
	if newIp == nil {
		u.log.Warn("Fetched external IP failed to parse %s", ipstr)
		return
	}
	oldIp := u.ip.Ip().IP
	u.ip.UpdateIP(newIp)
	if !oldIp.Equal(newIp) {
		u.log.Info("ExternalIP updated to %s", newIp)
	}
}
