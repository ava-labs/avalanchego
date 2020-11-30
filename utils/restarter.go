package utils

// Restarter causes the node to shutdown and restart
type Restarter interface {
	Restart()
}
