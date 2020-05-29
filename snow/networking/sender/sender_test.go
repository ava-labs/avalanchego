// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/utils/logging"
)

func TestSenderContext(t *testing.T) {
	context := snow.DefaultContextTest()
	sender := Sender{}
	sender.Initialize(
		context,
		&ExternalSenderTest{},
		&router.ChainRouter{},
		&timeout.Manager{},
	)
	if res := sender.Context(); !reflect.DeepEqual(res, context) {
		t.Fatalf("Got %#v, expected %#v", res, context)
	}
}

func TestTimeout(t *testing.T) {
	tm := timeout.Manager{}
	tm.Initialize(time.Millisecond)
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}
	chainRouter.Initialize(logging.NoLog{}, &tm, time.Hour)

	sender := Sender{}
	sender.Initialize(snow.DefaultContextTest(), &ExternalSenderTest{}, &chainRouter, &tm)

	engine := common.EngineTest{T: t}
	engine.Default(true)

	engine.ContextF = snow.DefaultContextTest

	wg := sync.WaitGroup{}
	wg.Add(2)

	failedVDRs := ids.ShortSet{}
	engine.QueryFailedF = func(validatorID ids.ShortID, _ uint32) error {
		failedVDRs.Add(validatorID)
		wg.Done()
		return nil
	}

	handler := router.Handler{}
	handler.Initialize(&engine, nil, 1)
	go handler.Dispatch()

	chainRouter.AddChain(&handler)

	vdrIDs := ids.ShortSet{}
	vdrIDs.Add(ids.NewShortID([20]byte{255}))
	vdrIDs.Add(ids.NewShortID([20]byte{254}))

	sender.PullQuery(vdrIDs, 0, ids.Empty)

	wg.Wait()

	if !failedVDRs.Equals(vdrIDs) {
		t.Fatalf("Timeouts should have fired")
	}
}
