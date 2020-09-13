package timer

import "testing"

func TestExecutor(t *testing.T) {
	executor := Executor{}
	executor.Initialize()
	go executor.Dispatch()

	called := make(chan struct{}, 1)
	f := func() {
		called <- struct{}{}
	}
	executor.Add(f)
	// The second call to f will block until the channel has
	// been read from, but that should not cause Add to block
	executor.Add(f)

	<-called
	<-called
	executor.Stop()
}
