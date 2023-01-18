package proposer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	NumProposers = 6
)

type Retriever interface {
	GetCurrentProposers() (set.Set[ids.NodeID], error)
}