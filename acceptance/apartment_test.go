//go:build windows && amd64

package acceptance

import (
	"runtime"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// enterApartment initializes WinRT for a test that calls plain WinRT APIs directly,
// rather than going through app.Run.
//
// The lock is the point, and it is not for this test's benefit.
//
// WinRT apartments are per THREAD. Go multiplexes goroutines over threads, so a test
// that initializes an apartment and then returns leaves that thread initialized and
// back in the pool — and the next test to land on it may be one calling app.Run, which
// needs a single-threaded apartment and gets RPC_E_CHANGED_MODE, "cannot change thread
// mode after it is set". Whether that happens depends on which thread the scheduler
// hands out, so it is a flake rather than a failure: the suite passes until it does not.
//
// runtime.LockOSThread with no matching Unlock pins this goroutine to a thread AND
// tells the Go runtime to destroy that thread when the goroutine exits. The apartment
// therefore cannot outlive the test that entered it.
//
// The caller must also avoid t.Run: a subtest runs on its own goroutine, which is a
// different thread, where this apartment does not exist.
func enterApartment(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	if err := winrt.Initialize(); err != nil {
		t.Skipf("no WinRT apartment available: %v", err)
	}
}
