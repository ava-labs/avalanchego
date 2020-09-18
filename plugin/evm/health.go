package evm

// Health returns nil if this chain is healthy.
// Also returns details, which should be one of:
// string, []byte, map[string]string
func (vm *VM) Health() (interface{}, error) {
	// TODO perform actual health check
	return nil, nil
}
