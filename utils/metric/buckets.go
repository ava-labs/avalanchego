// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	// Useful latency buckets

	MillisecondsBuckets = []float64{
		10,    // 10 ms is ~ instant
		100,   // 100 ms
		250,   // 250 ms
		500,   // 500 ms
		1000,  // 1 second
		1500,  // 1.5 seconds
		2000,  // 2 seconds
		3000,  // 3 seconds
		5000,  // 5 seconds
		10000, // 10 seconds
		// anything larger than 10 seconds will be bucketed together
	}
	NanosecondsBuckets = []float64{
		float64(100 * time.Nanosecond),
		float64(time.Microsecond),
		float64(10 * time.Microsecond),
		float64(100 * time.Microsecond),
		float64(time.Millisecond),
		float64(10 * time.Millisecond),
		float64(100 * time.Millisecond),
		float64(time.Second),
		// anything larger than a second will be bucketed together
	}
	MillisecondsHTTPBuckets = []float64{
		100,  // 100 ms - instant
		250,  // 250 ms - good
		500,  // 500 ms - not great
		1000, // 1 second - worrisome
		5000, // 5 seconds - bad
		// anything larger than 5 seconds will be bucketed together
	}

	// Useful bytes buckets

	BytesBuckets = []float64{
		256,
		units.KiB,
		4 * units.KiB,
		16 * units.KiB,
		64 * units.KiB,
		256 * units.KiB,
		units.MiB,
		4 * units.MiB,
		// anything larger than 4 MiB will be bucketed together
	}
)
