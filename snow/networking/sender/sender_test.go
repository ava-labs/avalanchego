// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/networking/handler"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/utils/logging"
)

func TestTimeout(t *testing.T) {
	tm := timeout.Manager{}
	tm.Initialize(time.Millisecond)
	go tm.Dispatch()

	router := router.ChainRouter{}
	router.Initialize(logging.NoLog{}, &tm)

	sender := Sender{}
	sender.Initialize(snow.DefaultContextTest(), &ExternalSenderTest{}, &router, &tm)

	engine := common.EngineTest{T: t}
	engine.Default(true)

	engine.ContextF = snow.DefaultContextTest

	wg := sync.WaitGroup{}
	wg.Add(2)

	failedVDRs := ids.ShortSet{}
	engine.QueryFailedF = func(validatorID ids.ShortID, _ uint32) {
		failedVDRs.Add(validatorID)
		wg.Done()
	}

	handler := handler.Handler{}
	handler.Initialize(&engine, nil, 1)
	go handler.Dispatch()

	router.AddChain(&handler)

	vdrIDs := ids.ShortSet{}
	vdrIDs.Add(ids.NewShortID([20]byte{255}))
	vdrIDs.Add(ids.NewShortID([20]byte{254}))

	sender.PullQuery(vdrIDs, 0, ids.Empty)

	wg.Wait()

	if !failedVDRs.Equals(vdrIDs) {
		t.Fatalf("Timeouts should have fired")
	}
}
