// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"sync"
	"time"

	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	defaultMappingTimeout        = 30 * time.Minute
	defaultMappingUpdateInterval = 3 * defaultMappingTimeout / 4
)

// Mapper maps port
type Mapper interface {
	MapPort(newInternalPort, newExternalPort uint16) error
	UnmapAllPorts() error
}

type mapper struct {
	log                   logging.Logger
	router                Router
	networkProtocol       NetworkProtocol
	mappingNames          string
	mappingTimeout        time.Duration
	mappingUpdateInterval time.Duration

	closer  chan struct{}
	wg      sync.WaitGroup
	errLock sync.Mutex
	errs    wrappers.Errs
}

// NewMapper returns a new mapper that can map ports on a router
func NewMapper(
	log logging.Logger,
	router Router,
	networkProtocol NetworkProtocol,
	mappingNames string,
	mappingTimeout time.Duration,
	mappingUpdateInterval time.Duration,
) Mapper {
	return &mapper{
		log:                   log,
		router:                router,
		networkProtocol:       networkProtocol,
		mappingNames:          mappingNames,
		mappingTimeout:        mappingTimeout,
		mappingUpdateInterval: mappingUpdateInterval,
		closer:                make(chan struct{}),
	}
}

// NewDefaultMapper returns a new mapper that can map ports on a router with
// default settings
func NewDefaultMapper(
	log logging.Logger,
	router Router,
	networkProtocol NetworkProtocol,
	mappingNames string,
) Mapper {
	return NewMapper(
		log,
		router,
		networkProtocol,
		mappingNames,
		defaultMappingTimeout,        // uses the default value
		defaultMappingUpdateInterval, // uses the default value
	)
}

// MapPort maps a local port to a port on the router until UnmapAllPorts is
// called.
func (m *mapper) MapPort(newInternalPort, newExternalPort uint16) error {
	m.wg.Add(1)
	go m.mapPort(newInternalPort, newExternalPort)
	return nil
}

func (m *mapper) mapPort(newInternalPort, newExternalPort uint16) {
	// duration is set to 0 here so that the select case will execute
	// immediately
	updateTimer := time.NewTimer(0)
	defer func() {
		updateTimer.Stop()

		m.errLock.Lock()
		m.errs.Add(m.router.UnmapPort(
			m.networkProtocol,
			newInternalPort,
			newExternalPort))
		m.errLock.Unlock()

		m.log.Debug("Unmapped external port %d to internal port %d",
			newExternalPort,
			newInternalPort)

		m.wg.Done()
	}()

	for {
		select {
		case <-updateTimer.C:
			err := m.router.MapPort(
				m.networkProtocol,
				newInternalPort,
				newExternalPort,
				m.mappingNames,
				m.mappingTimeout)

			if err != nil {
				m.errLock.Lock()
				m.errs.Add(err)
				m.errLock.Unlock()

				m.log.Debug("Failed to add mapping from external port %d to internal port %d due to %s",
					newExternalPort,
					newInternalPort,
					err)
			} else {
				m.log.Debug("Mapped external port %d to internal port %d",
					newExternalPort,
					newInternalPort)
			}

			// remap the port in m.mappingUpdateInterval
			updateTimer.Reset(m.mappingUpdateInterval)
		case _, _ = <-m.closer:
			return // only return when all ports are unmapped
		}
	}
}

func (m *mapper) UnmapAllPorts() error {
	close(m.closer)
	m.wg.Wait()
	return m.errs.Err
}
