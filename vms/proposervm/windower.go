package proposervm

import (
	"fmt"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// Validators custom ordering
type validatorData struct {
	ID     ids.ShortID
	weight uint64
}

type validatorsSlice []validatorData

// sort.Interface.
func (d validatorsSlice) Len() int {
	return len(d)
}

// sort.Interface.
func (d validatorsSlice) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

// sort.Interface. Sorting by decreasing weight
func (d validatorsSlice) Less(i, j int) bool {
	return d[i].weight > d[j].weight
}

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
	vMap, err := w.VM.GetValidatorSet(pChainHeight, w.subnetID)
	if err != nil {
		return BlkSubmissionTolerance
	}

	// canonically sort validator by weight
	validators := make(validatorsSlice, 0, len(vMap))
	for k, v := range vMap {
		validators = append(validators, validatorData{
			ID:     k,
			weight: v,
		})
	}
	sort.Sort(validators)

	totalValCount := uint64(len(validators))
	valPos := uint64(0)
	for _, val := range validators {
		if val.ID == valID {
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
