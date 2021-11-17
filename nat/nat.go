// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	mapTimeout        = 30 * time.Minute
	maxRefreshRetries = 3
)

// Router describes the functionality that a network device must support to be
// able to open ports to an external IP.
type Router interface {
	// True iff this router supports NAT
	SupportsNAT() bool
	// Map external port [extPort] to internal port [intPort] for [duration]
	MapPort(protocol string, intPort, extPort uint16, desc string, duration time.Duration) error
	// Undo a port mapping
	UnmapPort(protocol string, intPort, extPort uint16) error
	// Return our external IP
	ExternalIP() (net.IP, error)
}

// GetRouter returns a router on the current network.
func GetRouter() Router {
	if r := getUPnPRouter(); r != nil {
		return r
	}
	if r := getPMPRouter(); r != nil {
		return r
	}

	return NewNoRouter()
}

// Mapper attempts to open a set of ports on a router
type Mapper struct {
	log    logging.Logger
	r      Router
	closer chan struct{}
	wg     sync.WaitGroup
}

// NewPortMapper returns an initialized mapper
func NewPortMapper(log logging.Logger, r Router) Mapper {
	return Mapper{
		log:    log,
		r:      r,
		closer: make(chan struct{}),
	}
}

// Map external port [extPort] (exposed to the internet) to internal port [intPort] (where our process is listening)
// and set [ip]. Does this every [updateTime]. [ip] may be nil.
func (m *Mapper) Map(protocol string, intPort, extPort uint16, desc string, ip *utils.DynamicIPDesc, updateTime time.Duration) {
	if !m.r.SupportsNAT() {
		return
	}

	// we attempt a port map, and log an Error if it fails.
	err := m.retryMapPort(protocol, intPort, extPort, desc, mapTimeout)
	if err != nil {
		m.log.Error("NAT Traversal failed from external port %d to internal port %d with %s", extPort, intPort, err)
	} else {
		m.log.Info("NAT Traversal successful from external port %d to internal port %d", extPort, intPort)
	}

	go m.keepPortMapping(protocol, intPort, extPort, desc, ip, updateTime)
}

// Retry port map up to maxRefreshRetries with a 1 second delay
func (m *Mapper) retryMapPort(protocol string, intPort, extPort uint16, desc string, timeout time.Duration) error {
	var err error
	for retryCnt := 0; retryCnt < maxRefreshRetries; retryCnt++ {
		err = m.r.MapPort(protocol, intPort, extPort, desc, timeout)
		if err == nil {
			return nil
		}

		// log a message, sleep a second and retry.
		m.log.Error("Renewing port mapping try #%d from external port %d to internal port %d failed with %s",
			retryCnt+1, extPort, intPort, err)
		time.Sleep(1 * time.Second)
	}
	return err
}

// keepPortMapping runs in the background to keep a port mapped. It renews the mapping from [extPort]
// to [intPort]] every [updateTime]. Updates [ip] every [updateTime].
func (m *Mapper) keepPortMapping(protocol string, intPort, extPort uint16, desc string, ip *utils.DynamicIPDesc, updateTime time.Duration) {
	updateTimer := time.NewTimer(updateTime)

	m.wg.Add(1)

	defer func(extPort uint16) {
		updateTimer.Stop()

		m.log.Debug("Unmap protocol %s external port %d", protocol, extPort)
		if err := m.r.UnmapPort(protocol, intPort, extPort); err != nil {
			m.log.Debug("Error unmapping port %d to %d: %s", intPort, extPort, err)
		}

		m.wg.Done()
	}(extPort)

	for {
		select {
		case <-updateTimer.C:
			err := m.retryMapPort(protocol, intPort, extPort, desc, mapTimeout)
			if err != nil {
				m.log.Warn("Renew NAT Traversal failed from external port %d to internal port %d with %s",
					extPort, intPort, err)
			}
			m.updateIP(ip)
			updateTimer.Reset(updateTime)
		case <-m.closer:
			return
		}
	}
}

func (m *Mapper) updateIP(ip *utils.DynamicIPDesc) {
	if ip == nil {
		return
	}
	newIP, err := m.r.ExternalIP()
	if err != nil {
		m.log.Error("failed to get external IP: %s", err)
		return
	}
	oldIP := ip.IP().IP
	ip.UpdateIP(newIP)
	if !oldIP.Equal(newIP) {
		m.log.Info("external IP updated to: %s", newIP)
	}
}

// UnmapAllPorts stops mapping all ports from this mapper and attempts to unmap
// them.
func (m *Mapper) UnmapAllPorts() {
	close(m.closer)
	m.wg.Wait()
	m.log.Info("Unmapped all ports")
}
