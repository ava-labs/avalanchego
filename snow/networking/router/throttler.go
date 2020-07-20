package router

import (
	"github.com/ava-labs/gecko/ids"
)

// Throttler provides an interface to register consumption
// of resources and prioritize messages from nodes that have
// used less CPU time.
type Throttler interface {
	AddMessage(ids.ShortID)
	RemoveMessage(ids.ShortID)
	UtilizeCPU(ids.ShortID, float64)
	GetUtilization(ids.ShortID) (float64, bool) // Returns the CPU based priority and whether or not the peer has too many pending messages
	EndInterval()                               // Notify throttler that the current period has ended
}

type emptyThrottler struct{}

func (e *emptyThrottler) AddMessage(validatorID ids.ShortID) {}

func (e *emptyThrottler) RemoveMessage(validatorID ids.ShortID) {}

func (e *emptyThrottler) UtilizeCPU(validatorID ids.ShortID, consumption float64) {}

func (e *emptyThrottler) GetUtilization(validatorID ids.ShortID) (float64, uint32) { return 0, 0 }

func (e *emptyThrottler) EndInterval() {}
