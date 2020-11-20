package service

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/avm/internalvm"

	"github.com/ava-labs/avalanchego/vms/avm/vmargs"

	"github.com/ava-labs/avalanchego/utils/logging"

	vmerror "github.com/ava-labs/avalanchego/vms/components/error"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024
)

type Service struct {
	vm *internalvm.VM
}

func NewService(vm *internalvm.VM) *Service {
	return &Service{vm: vm}
}

// GetUTXOs gets all utxos for passed in addresses
func (service *Service) GetUTXOs(args *vmargs.GetUTXOsArgs) (*vmargs.GetUTXOsReply, error) {
	service.vm.Context().Log.Info("AVM: GetUTXOs called for with %s", args.Addresses)

	reply := &vmargs.GetUTXOsReply{}

	if len(args.Addresses) == 0 {
		return nil, vmerror.ErrNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return nil, fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	encoding, err := service.vm.EncodingManager().GetEncoding(args.Encoding)
	if err != nil {
		return nil, fmt.Errorf("problem getting encoding formatter for '%s': %w", args.Encoding, err)
	}

	var sourceChain ids.ID
	if args.SourceChain == "" {
		sourceChain = service.vm.Context().ChainID
	} else {
		chainID, err := service.vm.Context().BCLookup.Lookup(args.SourceChain)
		if err != nil {
			return nil, fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet := ids.ShortSet{}
	for _, addrStr := range args.Addresses {
		addr, err := service.vm.ParseLocalAddress(addrStr)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrSet.Add(addr)
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		addr, err := service.vm.ParseLocalAddress(args.StartIndex.Address)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		utxo, err := ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse start index utxo: %w", err)
		}

		startAddr = addr
		startUTXO = utxo
	}

	var (
		utxos     []*avax.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
	)
	if sourceChain == service.vm.Context().ChainID {
		utxos, endAddr, endUTXOID, err = service.vm.GetUTXOs(
			addrSet,
			startAddr,
			startUTXO,
			int(args.Limit),
			true,
		)
	} else {
		utxos, endAddr, endUTXOID, err = service.vm.GetAtomicUTXOs(
			sourceChain,
			addrSet,
			startAddr,
			startUTXO,
			int(args.Limit),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		b, err := service.vm.Codec().Marshal(utxo)
		if err != nil {
			return nil, fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		reply.UTXOs[i] = encoding.ConvertBytes(b)
	}

	endAddress, err := service.vm.FormatLocalAddress(endAddr)
	if err != nil {
		return nil, fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = json.Uint64(len(utxos))
	reply.Encoding = encoding.Encoding()
	return reply, nil
}

func (service *Service) Log() logging.Logger {

	return service.vm.Context().Log
}
