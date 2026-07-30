//go:build windows && amd64

package winui

import (
	"fmt"
	"runtime"
	"sync/atomic"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// uiThreadID records the thread the UI was entered on, so a handler can assert it
// is running where it must. Atomic because it is read from delegate bodies.
var uiThreadID atomic.Uint32

// The UI thread.
//
// XAML requires a single-threaded apartment and keeps its state per UI thread.
// These bindings dispatch straight through vtable slots with no COM proxy in
// between, so touching a XAML object from the wrong thread is an unmarshalled
// cross-apartment call — not a slow one, an invalid one.
//
// This file has no dependency on generated code, deliberately: it is the layer
// underneath the bindings, and the bindings' own package tree imports it.

// EnterUIThread pins the calling goroutine to its OS thread, puts that thread in a
// single-threaded apartment, registers it as the thread delegate bodies run on, and
// adds the Windows App SDK framework package to the process.
//
// The order is not interchangeable, and each step's reason is different:
//
//  1. runtime.LockOSThread, because a XAML object is only valid on the thread its
//     apartment belongs to, and a goroutine may otherwise be rescheduled onto
//     another.
//  2. RoInitialize(single-threaded), BEFORE anything is activated. Apartment
//     initialization is per thread and first-come: go-bindings-winrt would
//     otherwise default this thread to the multithreaded apartment on the first
//     activation, and XAML would refuse to run on it.
//  3. SetInlineThread, so a delegate body runs on the thread the framework invoked
//     it on. Handed to a new goroutine instead, a handler could not legally touch
//     the objects it was passed.
//  4. Bootstrap, which requires COM to be initialized already.
//
// The returned function reverses the bootstrap. The thread stays locked and stays
// in its apartment: XAML holds thread-affine state for the life of the process, and
// there is no way to hand it back.
func EnterUIThread() (release func(), err error) {
	return EnterUIThreadWith(DefaultBootstrap())
}

// EnterUIThreadWith is EnterUIThread with explicit bootstrap options — an explicit
// DLL path, a pre-release version tag, a minimum framework version.
func EnterUIThreadWith(options BootstrapOptions) (release func(), err error) {
	runtime.LockOSThread()

	if err := syswinrt.RoInitialize(syswinrt.RO_INIT_SINGLETHREADED); err != nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("winui: RoInitialize(single-threaded): %w", err)
	}

	current := winrt.CurrentThreadID()
	winrt.SetInlineThread(current)
	uiThreadID.Store(current)

	if err := Bootstrap(options); err != nil {
		winrt.SetInlineThread(0)
		uiThreadID.Store(0)
		runtime.UnlockOSThread()
		return nil, err
	}

	return func() {
		BootstrapShutdown()
		winrt.SetInlineThread(0)
		uiThreadID.Store(0)
	}, nil
}

// UIThreadID is the thread the UI was entered on, or 0 before EnterUIThread. A
// handler can compare it against winrt.CurrentThreadID to assert it is running
// where it must.
func UIThreadID() uint32 { return uiThreadID.Load() }
