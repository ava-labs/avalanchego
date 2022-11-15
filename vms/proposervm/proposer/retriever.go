package proposer

import (
	"github.com/ava-labs/avalanchego/ids"
)

const (
	NumProposers = 6
)

type Retriever interface {
	GetCurrentProposers() (ids.NodeIDSet, error)
}

