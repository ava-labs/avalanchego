// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/cache"
	"github.com/chain4travel/caminogo/utils/timer/mockable"
)

var _ cache.Cacher = &Cache{}

type Cache struct {
	metrics
	cache.Cacher

	clock mockable.Clock
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	cache cache.Cacher,
) (cache.Cacher, error) {
	meterCache := &Cache{Cacher: cache}
	return meterCache, meterCache.metrics.Initialize(namespace, registerer)
}

func (c *Cache) Put(key, value interface{}) {
	start := c.clock.Time()
	c.Cacher.Put(key, value)
	end := c.clock.Time()
	c.put.Observe(float64(end.Sub(start)))
}

func (c *Cache) Get(key interface{}) (interface{}, bool) {
	start := c.clock.Time()
	value, has := c.Cacher.Get(key)
	end := c.clock.Time()
	c.get.Observe(float64(end.Sub(start)))
	if has {
		c.hit.Inc()
	} else {
		c.miss.Inc()
	}

	return value, has
}
