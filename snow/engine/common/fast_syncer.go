package common

type FastSyncer interface {
	Engine
	IsEnabled() bool
}
