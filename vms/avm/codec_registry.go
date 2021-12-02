// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"math"
	"reflect"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	codecVersion = 0
)

var (
	_ codec.Registry = &codecRegistry{}
	_ secp256k1fx.VM = &fxVM{}
)

type codecRegistry struct {
	codecs      []codec.Registry
	index       int
	typeToIndex map[reflect.Type]int
}

func (cr *codecRegistry) RegisterType(val interface{}) error {
	valType := reflect.TypeOf(val)
	cr.typeToIndex[valType] = cr.index

	errs := wrappers.Errs{}
	for _, c := range cr.codecs {
		errs.Add(c.RegisterType(val))
	}
	return errs.Err
}

// NewCodecs returns the genesis codec and the normal codec for the provided
// feature extensions.
func NewCodecs(fxs []Fx) (codec.Manager, codec.Manager, error) {
	return newCustomCodecs(
		make(map[reflect.Type]int),
		&mockable.Clock{},
		logging.NoLog{},
		fxs,
	)
}

func newCustomCodecs(
	typeToFxIndex map[reflect.Type]int,
	clock *mockable.Clock,
	log logging.Logger,
	fxs []Fx,
) (codec.Manager, codec.Manager, error) {
	gc := linearcodec.New(reflectcodec.DefaultTagName, 1<<20)
	c := linearcodec.NewDefault()

	gcm := codec.NewManager(math.MaxInt32)
	cm := codec.NewDefaultManager()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&BaseTx{}),
		c.RegisterType(&CreateAssetTx{}),
		c.RegisterType(&OperationTx{}),
		c.RegisterType(&ImportTx{}),
		c.RegisterType(&ExportTx{}),
		cm.RegisterCodec(codecVersion, c),

		gc.RegisterType(&BaseTx{}),
		gc.RegisterType(&CreateAssetTx{}),
		gc.RegisterType(&OperationTx{}),
		gc.RegisterType(&ImportTx{}),
		gc.RegisterType(&ExportTx{}),
		gcm.RegisterCodec(codecVersion, gc),
	)
	if errs.Errored() {
		return nil, nil, errs.Err
	}

	vm := &fxVM{
		typeToFxIndex: typeToFxIndex,
		clock:         clock,
		log:           log,
	}
	for i, fx := range fxs {
		vm.codecRegistry = &codecRegistry{
			codecs:      []codec.Registry{gc, c},
			index:       i,
			typeToIndex: vm.typeToFxIndex,
		}
		if err := fx.Initialize(vm); err != nil {
			return nil, nil, err
		}
	}
	return gcm, cm, nil
}

type fxVM struct {
	typeToFxIndex map[reflect.Type]int

	clock         *mockable.Clock
	log           logging.Logger
	codecRegistry codec.Registry
}

func (vm *fxVM) Clock() *mockable.Clock        { return vm.clock }
func (vm *fxVM) CodecRegistry() codec.Registry { return vm.codecRegistry }
func (vm *fxVM) Logger() logging.Logger        { return vm.log }
