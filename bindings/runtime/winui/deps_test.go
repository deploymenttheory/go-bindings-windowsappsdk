//go:build windows && amd64

package winui

import (
	"runtime"
	"testing"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	systemthreading "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/threading"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

// This repository is the fourth layer of a stack: go-bindings-win32 supplies
// the COM and WinRT ABI, go-bindings-winrt projects the Windows.* namespaces
// on top of it, and the Windows App SDK bindings will sit on both. Nothing
// else checks that those three actually compose in a fresh module at the
// versions go.mod pins, and a mismatch there would surface much later as
// something far less obvious than a failing test.

// TestWinRTActivationThroughTheStack activates a real Windows.* runtime class
// through the pinned go-bindings-winrt, which exercises the whole chain:
// apartment initialization, the activation factory lookup, an HSTRING read,
// and reference counting.
func TestWinRTActivationThroughTheStack(t *testing.T) {
	calendar, err := globalization.NewCalendar()
	if err != nil {
		t.Fatalf("NewCalendar: %v", err)
	}
	defer calendar.Release()

	name, err := calendar.GetCalendarSystem()
	if err != nil {
		t.Fatalf("GetCalendarSystem: %v", err)
	}
	if name == "" {
		t.Error("calendar system is empty; the HSTRING path is not working")
	}
}

// TestApartmentInitIsPerThread depends on the per-thread Initialize added in
// go-bindings-winrt v0.4.0. Against the previous process-wide guard the
// second thread here would be left uninitialized. Since the UI thread this
// repository will introduce is a separate, deliberately single-threaded
// apartment, that behaviour is load-bearing rather than incidental.
func TestApartmentInitIsPerThread(t *testing.T) {
	if err := winrt.Initialize(); err != nil {
		t.Fatalf("Initialize on the test thread: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		result <- winrt.Initialize()
	}()
	if err := <-result; err != nil {
		t.Fatalf("Initialize on a second thread: %v", err)
	}
}

// TestABIFoundationIsReachable pins the layering rule the generated code will
// rely on: the ABI types come from go-bindings-win32 and are never redeclared
// here.
func TestABIFoundationIsReachable(t *testing.T) {
	if got := win32.HRESULT(0); got.Failed() {
		t.Errorf("HRESULT(0).Failed() = true, want false")
	}
	if systemthreading.GetCurrentThreadId() == 0 {
		t.Error("GetCurrentThreadId returned 0")
	}
}
