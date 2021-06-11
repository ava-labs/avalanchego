package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// windower interfaces with P-Chain and it is responsible for:
// retrieving current P-Chain height
// calculate the start time for the block submission window of a given validator

type windower struct {
	dummyPChainHeight uint64 // mock until P-Chain is integrated
	mockedValPos      uint
}

func (w *windower) pChainHeight() uint64 {
	// TODO: call platformVM.LastAccepted().Height instead of mock
	return w.dummyPChainHeight
}

func (w *windower) BlkSubmissionDelay(pChainHeight uint64, valID ids.ID) time.Duration {
	// TODO:
	//       pick validators population at given pChainHeight
	//       if valID not in validator set, valPos = len(validators population)
	//       else pick random permutation, seed by pChainHeight???
	//       valPos is valID position in the permutation
	return time.Duration(w.mockedValPos) * BlkSubmissionWinLength
}
