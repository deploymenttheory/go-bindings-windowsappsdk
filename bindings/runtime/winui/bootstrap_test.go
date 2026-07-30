//go:build windows && amd64

package winui

import (
	"errors"
	"runtime"
	"testing"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// These are the M1 gate. Everything this repository plans to generate assumes
// a Go executable can add the Windows App SDK framework package to its own
// process and then activate Microsoft.UI.* types. If that is not true, no
// amount of generated code helps, so it is worth proving before writing any.
//
// They need the bootstrapper next to them: `go run ./cmd/fetch-bootstrap`.
// Its absence is skipped rather than failed — the DLL is deliberately not
// committed, so not having fetched it is a fact about the checkout and not a
// regression in anything. Every other failure is real.

// requireBootstrap adds the framework package to this process, or skips when
// the redistributable has not been fetched.
func requireBootstrap(t *testing.T) {
	t.Helper()
	err := Bootstrap(DefaultBootstrap())
	var notFound *ErrBootstrapDLLNotFound
	if errors.As(err, &notFound) {
		t.Skipf("bootstrapper not fetched; run `go run ./cmd/fetch-bootstrap` (looked in %v)", notFound.Searched)
	}
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(BootstrapShutdown)
}

func TestPackVersion(t *testing.T) {
	// The layout PACKAGE_VERSION uses: major, minor, build, revision, each
	// sixteen bits, most significant first.
	got := PackVersion(2, 3, 1, 0)
	if want := PackageVersion(0x0002_0003_0001_0000); got != want {
		t.Errorf("PackVersion(2,3,1,0) = %#016x, want %#016x", uint64(got), uint64(want))
	}
	if got.String() != "2.3.1.0" {
		t.Errorf("String() = %q, want 2.3.1.0", got.String())
	}
}

// TestBootstrapAndActivateXaml is the gate itself.
func TestBootstrapAndActivateXaml(t *testing.T) {
	// The bootstrapper requires COM on this thread, and XAML requires a
	// single-threaded apartment, so take both now rather than letting the
	// activation path pick the multithreaded default.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := syswinrt.RoInitialize(syswinrt.RO_INIT_SINGLETHREADED); err != nil {
		t.Fatalf("RoInitialize(single-threaded): %v", err)
	}

	requireBootstrap(t)
	t.Logf("bootstrapped using %s", BootstrapDLLPath())

	// Before the bootstrap this fails: nothing registers Microsoft.UI.* with
	// the operating system. Afterwards the framework package is in this
	// process's package graph and the activation factory resolves.
	for _, class := range []string{
		"Microsoft.UI.Xaml.Application",
		"Microsoft.UI.Xaml.Window",
		"Microsoft.UI.Xaml.Controls.Button",
	} {
		factory, err := winrt.GetActivationFactory(class, &syswinrt.IID_IInspectable)
		if err != nil {
			t.Errorf("GetActivationFactory(%s): %v", class, err)
			continue
		}
		if factory == nil {
			t.Errorf("GetActivationFactory(%s) returned nil", class)
			continue
		}
		factory.Release()
		t.Logf("activation factory resolved: %s", class)
	}
}

// TestBootstrapIsIdempotent guards the lifecycle: an application may call it
// defensively, and a second call must not take a second reference on the
// framework package.
func TestBootstrapIsIdempotent(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := syswinrt.RoInitialize(syswinrt.RO_INIT_SINGLETHREADED); err != nil {
		t.Fatalf("RoInitialize: %v", err)
	}

	requireBootstrap(t)
	if err := Bootstrap(DefaultBootstrap()); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	if !Bootstrapped() {
		t.Error("Bootstrapped() is false after a successful Bootstrap")
	}
}

// TestBootstrapMissingDLLIsDiagnosable checks the failure a new user is most
// likely to hit first: the redistributable is simply not there.
func TestBootstrapMissingDLLIsDiagnosable(t *testing.T) {
	// findBootstrapDLL rather than loadBootstrapDLL: the latter short-circuits
	// once the library is loaded, which another test in this package will
	// already have done.
	_, err := findBootstrapDLL(`Z:\definitely\not\here\` + bootstrapDLLName)
	if err == nil {
		t.Fatal("locating a non-existent bootstrapper succeeded")
	}
	var notFound *ErrBootstrapDLLNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("got %v, want ErrBootstrapDLLNotFound", err)
	}
	if len(notFound.Searched) == 0 {
		t.Error("the error does not say where it looked")
	}
}
