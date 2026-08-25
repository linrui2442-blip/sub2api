package personal

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopExitStopsRunningGatewayBeforeQuit(t *testing.T) {
	var running atomic.Bool
	running.Store(true)
	quit := make(chan struct{})
	handler := newDesktopExitHandler(func() error {
		running.Store(false)
		return nil
	}, func() {
		if running.Load() {
			t.Error("desktop quit ran before gateway stop completed")
		}
		close(quit)
	})

	handler.Exit()

	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("desktop runtime did not quit")
	}
	if running.Load() {
		t.Fatal("gateway remained running after desktop exit")
	}
}

func TestDesktopExitWhenGatewayAlreadyStopped(t *testing.T) {
	var stopCalls atomic.Int32
	var quitCalls atomic.Int32
	handler := newDesktopExitHandler(func() error {
		stopCalls.Add(1)
		return nil
	}, func() { quitCalls.Add(1) })

	handler.Exit()

	if got := stopCalls.Load(); got != 1 {
		t.Fatalf("stop calls = %d, want 1", got)
	}
	if got := quitCalls.Load(); got != 1 {
		t.Fatalf("quit calls = %d, want 1", got)
	}
}

func TestDesktopExitIsIdempotent(t *testing.T) {
	var stopCalls atomic.Int32
	var quitCalls atomic.Int32
	handler := newDesktopExitHandler(func() error {
		stopCalls.Add(1)
		return nil
	}, func() { quitCalls.Add(1) })

	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			handler.Exit()
		}()
	}
	callers.Wait()

	if got := stopCalls.Load(); got != 1 {
		t.Fatalf("stop calls = %d, want 1", got)
	}
	if got := quitCalls.Load(); got != 1 {
		t.Fatalf("quit calls = %d, want 1", got)
	}
}
