// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"io"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/utils/logging"
)

// Callable ...
type Callable interface {
	Call(writer http.ResponseWriter, method, base, endpoint string, body io.Reader, headers map[string]string) error
}

// Keystore ...
type Keystore interface {
	GetDatabase(username, password string) (database.Database, error)
}

// SharedMemory ...
type SharedMemory interface {
	GetDatabase(id ids.ID) database.Database
	ReleaseDatabase(id ids.ID)
}

// AliasLookup ...
type AliasLookup interface {
	Lookup(alias string) (ids.ID, error)
	PrimaryAlias(id ids.ID) (string, error)
}

// SubnetLookup ...
type SubnetLookup interface {
	SubnetID(chainID ids.ID) (ids.ID, error)
}

// Context is information about the current execution.
// [NetworkID] is the ID of the network this context exists within.
// [ChainID] is the ID of the chain this context exists within.
// [NodeID] is the ID of this node
type Context struct {
	NetworkID           uint32
	SubnetID            ids.ID
	ChainID             ids.ID
	NodeID              ids.ShortID
	Log                 logging.Logger
	DecisionDispatcher  *triggers.EventDispatcher
	ConsensusDispatcher *triggers.EventDispatcher
	Lock                sync.RWMutex
	Keystore            Keystore
	SharedMemory        SharedMemory
	BCLookup            AliasLookup
	SNLookup            SubnetLookup

	Namespace string
	Metrics   prometheus.Registerer
}

// DefaultContextTest ...
func DefaultContextTest() *Context {
	decisionED := triggers.EventDispatcher{}
	decisionED.Initialize(logging.NoLog{})
	consensusED := triggers.EventDispatcher{}
	consensusED.Initialize(logging.NoLog{})
	aliaser := &ids.Aliaser{}
	aliaser.Initialize()
	return &Context{
		NetworkID:           0,
		SubnetID:            ids.Empty,
		ChainID:             ids.Empty,
		NodeID:              ids.ShortEmpty,
		Log:                 logging.NoLog{},
		DecisionDispatcher:  &decisionED,
		ConsensusDispatcher: &consensusED,
		BCLookup:            aliaser,
		Namespace:           "",
		Metrics:             prometheus.NewRegistry(),
	}
}
