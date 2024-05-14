package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Sampler = (*StakeWeightedSampler)(nil)
)

type ValidatorSamplerOption interface {
	apply(options *validatorSamplerOptions)
}

type validatorSamplerOptionFunc func(options *validatorSamplerOptions)

func (o validatorSamplerOptionFunc) apply(options *validatorSamplerOptions) {
	o(options)
}

type validatorSamplerOptions struct {
	validatorSampler Sampler
}

func WithPeerSampler(peers *Peers) ClientOption {
	return clientOptionFunc(func(options *clientOptions) {
		options.sampler = sampler
	})
}

func WithUniformValidatorSampler(validators *Validators) ClientOption {
	return clientOptionFunc(func(options *clientOptions) {
		options.sampler = sampler
	})
}

func WithStakeWeightedSampler(validators *Validators) ClientOption {
	return clientOptionFunc(func(options *clientOptions) {
		options.sampler = sampler
	})
}

type Sampler interface {
	Sample(ctx context.Context, limit int, filters ...SamplingFilter) []ids.NodeID
}

type StakeWeightedSampler struct{}

func (s StakeWeightedSampler) Sample(ctx context.Context, limit int) []ids.NodeID {
	//TODO implement me
	panic("implement me")
}

type UniformPeerSampler struct{}

func (s StakeWeightedSampler) Sample(ctx context.Context, limit int) []ids.NodeID {
	//TODO implement me
	panic("implement me")
}
