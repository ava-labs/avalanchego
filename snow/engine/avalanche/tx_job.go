// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
)

type txParser struct {
	numAccepted, numDropped prometheus.Counter
	vm                      DAGVM
}

func (p *txParser) Parse(txBytes []byte) (queue.Job, error) {
	tx, err := p.vm.ParseTx(txBytes)
	if err != nil {
		return nil, err
	}
	return &txJob{
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		tx:          tx,
	}, nil
}

type txJob struct {
	numAccepted, numDropped prometheus.Counter
	tx                      snowstorm.Tx
}

func (t *txJob) ID() ids.ID { return t.tx.ID() }
func (t *txJob) MissingDependencies() ids.Set {
	missing := ids.Set{}
	for _, dep := range t.tx.Dependencies() {
		if dep.Status() != choices.Accepted {
			missing.Add(dep.ID())
		}
	}
	return missing
}
func (t *txJob) Execute() {
	if t.MissingDependencies().Len() != 0 {
		t.numDropped.Inc()
		return
	}

	switch t.tx.Status() {
	case choices.Unknown, choices.Rejected:
		t.numDropped.Inc()
	case choices.Processing:
		t.tx.Verify()
		t.tx.Accept()
		t.numAccepted.Inc()
	}
}
func (t *txJob) Bytes() []byte { return t.tx.Bytes() }
