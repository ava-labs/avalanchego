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

	<-called
	executor.Stop()
}
