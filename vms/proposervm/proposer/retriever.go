package proposer

import (
	"github.com/avalanchego/ids"
)

const (
	NumProposers = 6
)

type ProposerRetriever interface {
	GetCurrentProposers() (ids.NodeIDSet, error)
}

