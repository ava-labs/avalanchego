// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ cache.Cacher[struct{}, struct{}] = (*Cache[struct{}, struct{}])(nil)

type Cache[K comparable, V any] struct {
	metrics
	cache.Cacher[K, V]

	clock mockable.Clock
}

func New[K comparable, V any](
	namespace string,
	registerer prometheus.Registerer,
	cache cache.Cacher[K, V],
) (cache.Cacher[K, V], error) {
	meterCache := &Cache[K, V]{Cacher: cache}
	return meterCache, meterCache.metrics.Initialize(namespace, registerer)
}

func (c *Cache[K, V]) Put(key K, value V) {
	start := c.clock.Time()
	c.Cacher.Put(key, value)
	end := c.clock.Time()
	c.put.Observe(float64(end.Sub(start)))
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
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
