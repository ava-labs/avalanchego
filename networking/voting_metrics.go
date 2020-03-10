// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/logging"
)

type votingMetrics struct {
	numGetAcceptedFrontierSent, numGetAcceptedFrontierReceived,
	numAcceptedFrontierSent, numAcceptedFrontierReceived,
	numGetAcceptedSent, numGetAcceptedReceived,
	numAcceptedSent, numAcceptedReceived,
	numGetSent, numGetReceived,
	numPutSent, numPutReceived,
	numPushQuerySent, numPushQueryReceived,
	numPullQuerySent, numPullQueryReceived,
	numChitsSent, numChitsReceived prometheus.Counter
}

func (vm *votingMetrics) Initialize(log logging.Logger, registerer prometheus.Registerer) {
	vm.numGetAcceptedFrontierSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_accepted_frontier_sent",
			Help:      "Number of get accepted frontier messages sent",
		})
	vm.numGetAcceptedFrontierReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_accepted_frontier_received",
			Help:      "Number of get accepted frontier messages received",
		})
	vm.numAcceptedFrontierSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "accepted_frontier_sent",
			Help:      "Number of accepted frontier messages sent",
		})
	vm.numAcceptedFrontierReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "accepted_frontier_received",
			Help:      "Number of accepted frontier messages received",
		})
	vm.numGetAcceptedSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_accepted_sent",
			Help:      "Number of get accepted messages sent",
		})
	vm.numGetAcceptedReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_accepted_received",
			Help:      "Number of get accepted messages received",
		})
	vm.numAcceptedSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "accepted_sent",
			Help:      "Number of accepted messages sent",
		})
	vm.numAcceptedReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "accepted_received",
			Help:      "Number of accepted messages received",
		})
	vm.numGetSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_sent",
			Help:      "Number of get messages sent",
		})
	vm.numGetReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_received",
			Help:      "Number of get messages received",
		})
	vm.numPutSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "put_sent",
			Help:      "Number of put messages sent",
		})
	vm.numPutReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "put_received",
			Help:      "Number of put messages received",
		})
	vm.numPushQuerySent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "push_query_sent",
			Help:      "Number of push query messages sent",
		})
	vm.numPushQueryReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "push_query_received",
			Help:      "Number of push query messages received",
		})
	vm.numPullQuerySent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "pull_query_sent",
			Help:      "Number of pull query messages sent",
		})
	vm.numPullQueryReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "pull_query_received",
			Help:      "Number of pull query messages received",
		})
	vm.numChitsSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "chits_sent",
			Help:      "Number of chits messages sent",
		})
	vm.numChitsReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "chits_received",
			Help:      "Number of chits messages received",
		})

	if err := registerer.Register(vm.numGetAcceptedFrontierSent); err != nil {
		log.Error("Failed to register get_accepted_frontier_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numGetAcceptedFrontierReceived); err != nil {
		log.Error("Failed to register get_accepted_frontier_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numAcceptedFrontierSent); err != nil {
		log.Error("Failed to register accepted_frontier_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numAcceptedFrontierReceived); err != nil {
		log.Error("Failed to register accepted_frontier_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numGetAcceptedSent); err != nil {
		log.Error("Failed to register get_accepted_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numGetAcceptedReceived); err != nil {
		log.Error("Failed to register get_accepted_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numAcceptedSent); err != nil {
		log.Error("Failed to register accepted_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numAcceptedReceived); err != nil {
		log.Error("Failed to register accepted_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numGetSent); err != nil {
		log.Error("Failed to register get_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numGetReceived); err != nil {
		log.Error("Failed to register get_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numPutSent); err != nil {
		log.Error("Failed to register put_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numPutReceived); err != nil {
		log.Error("Failed to register put_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numPushQuerySent); err != nil {
		log.Error("Failed to register push_query_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numPushQueryReceived); err != nil {
		log.Error("Failed to register push_query_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numPullQuerySent); err != nil {
		log.Error("Failed to register pull_query_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numPullQueryReceived); err != nil {
		log.Error("Failed to register pull_query_received statistics due to %s", err)
	}
	if err := registerer.Register(vm.numChitsSent); err != nil {
		log.Error("Failed to register chits_sent statistics due to %s", err)
	}
	if err := registerer.Register(vm.numChitsReceived); err != nil {
		log.Error("Failed to register chits_received statistics due to %s", err)
	}
}
