// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"math/rand"
	"testing"
	"time"
)

func TestMeter(t *testing.T) {
	m := NewMeter(time.Millisecond)
	m.Start()
	for i := 0; i < 1000; i++ {
		time.Sleep(time.Microsecond)
		t.Logf("%f", m.Read())
	}
	m.Stop()
	for i := 0; i < 1000; i++ {
		time.Sleep(time.Microsecond)
		t.Logf("%f", m.Read())
	}
	t.Fail()
}

func TestMeter2(t *testing.T) {
	m := NewMeter(time.Millisecond)
	for i := 0; i < 1000; i++ {
		if rand.Int31n(2) == 0 {
			m.Start()
		}
		time.Sleep(10 * time.Microsecond)
		m.Stop()
		t.Logf("%f", m.Read())
	}
	t.Fail()
}
