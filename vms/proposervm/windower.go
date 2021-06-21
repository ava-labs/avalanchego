package proposervm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// windower interfaces with P-Chain and it is responsible for:
// retrieving current P-Chain height
// calculate the start time for the block submission window of a given validator

type windower struct {
	validators.VM
	subnetID ids.ID
	sampler  sampler.Uniform
}

func (w *windower) initialize(vm *VM, ctx *snow.Context) error {
	if ctx.ValidatorVM != nil {
		vm.windower.VM = ctx.ValidatorVM
	} else {
		// a nil ctx.ValidatorVM is expected only if we are wrapping P-chain VM itself.
		// Then core VM must implement the validators.VM interface
		if valVM, ok := vm.ChainVM.(validators.VM); ok {
			vm.windower.VM = valVM
		} else {
			return fmt.Errorf("core VM does not implement validators.VM interface")
		}
	}
	vm.windower.subnetID = ctx.SubnetID
	w.sampler = sampler.NewUniform() // TODO: shouldn't it be by weight?

	return nil
}

func (w *windower) pChainHeight() (uint64, error) {
	return w.VM.GetCurrentHeight()
}

func (w *windower) BlkSubmissionDelay(pChainHeight uint64, valID ids.ShortID) time.Duration {
	valids, err := w.VM.GetValidatorSet(pChainHeight, w.subnetID)
	if err != nil {
		return BlkSubmissionTolerance
	}

	totalValCount := uint64(len(valids))
	valPos := uint64(0)
	for val := range valids {
		if val == valID {
			break
		}
		valPos++
	}

	if valPos != totalValCount {
		if err := w.sampler.Initialize(totalValCount); err != nil {
			return time.Duration(totalValCount) * BlkSubmissionWinLength
		}
		w.sampler.Seed(int64(pChainHeight))
		permut, err := w.sampler.Sample(int(totalValCount))
		if err != nil {
			return time.Duration(totalValCount) * BlkSubmissionWinLength
		}
		valPos = permut[valPos]
		w.sampler.ClearSeed()
	}

	return time.Duration(valPos) * BlkSubmissionWinLength
}
