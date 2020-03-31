// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
)

var (
	Red   = ids.Empty.Prefix(0)
	Blue  = ids.Empty.Prefix(1)
	Green = ids.Empty.Prefix(2)
)

func ParamsTest(t *testing.T, factory Factory) {
	sb := factory.New()

	params := Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2, ConcurrentRepolls: 1,
	}
	sb.Initialize(params, Red)

	if p := sb.Parameters(); p.K != params.K {
		t.Fatalf("Wrong K parameter")
	} else if p.Alpha != params.Alpha {
		t.Fatalf("Wrong Alpha parameter")
	} else if p.BetaVirtuous != params.BetaVirtuous {
		t.Fatalf("Wrong Beta1 parameter")
	} else if p.BetaRogue != params.BetaRogue {
		t.Fatalf("Wrong Beta2 parameter")
	} else if p.ConcurrentRepolls != params.ConcurrentRepolls {
		t.Fatalf("Wrong Repoll parameter")
	}
}
