package dynamicip

import (
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func FetchExternalIP() (string, error) {
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

type ExternalIPUpdaterInterface interface {
	Stop()
}

type NoExternalIPUpdater struct {
}

func (u *NoExternalIPUpdater) Stop() {
}

type ExternalIPUpdater struct {
	tickerCloser chan struct{}
	log          logging.Logger
	ip           *utils.DynamicIPDesc
}

func NewExternalIPUpdater(enable bool, updateTime time.Duration, log logging.Logger, ip *utils.DynamicIPDesc) ExternalIPUpdaterInterface {
	if enable {
		updater := &ExternalIPUpdater{log: log, ip: ip}
		go updater.UpdateExternalIP(updateTime)
		return updater
	}
	return &NoExternalIPUpdater{}
}

func (u *ExternalIPUpdater) Stop() {
	close(u.tickerCloser)
}

func (u *ExternalIPUpdater) UpdateExternalIP(frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ipstr, err := FetchExternalIP()
			if err != nil {
				u.log.Warn("Fetch external IP failed %s", err)
				continue
			}
			newIp := net.ParseIP(ipstr)
			if newIp == nil {
				u.log.Warn("Fetched external IP failed to parse %s", ipstr)
				continue
			}
			oldIp := u.ip.Ip().IP
			u.ip.UpdateIP(newIp)
			if !oldIp.Equal(newIp) {
				u.log.Info("ExternalIP updated to %s", newIp)
			}
		case <-u.tickerCloser:
			return
		}
	}
}
