// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ cache.Cacher[struct{}, struct{}] = (*Cache[struct{}, struct{}])(nil)

type Cache[T comparable, K any] struct {
	metrics
	cache.Cacher[T, K]

	clock mockable.Clock
}

func New[T comparable, K any](
	namespace string,
	registerer prometheus.Registerer,
	cache cache.Cacher[T, K],
) (cache.Cacher[T, K], error) {
	meterCache := &Cache[T, K]{Cacher: cache}
	return meterCache, meterCache.metrics.Initialize(namespace, registerer)
}

func (c *Cache[T, K]) Put(key T, value K) {
	start := c.clock.Time()
	c.Cacher.Put(key, value)
	end := c.clock.Time()
	c.put.Observe(float64(end.Sub(start)))
}

func (c *Cache[T, K]) Get(key T) (K, bool) {
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
