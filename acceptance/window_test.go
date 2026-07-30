//go:build windows && amd64

// Package acceptance drives the generated bindings against a live Windows App SDK.
//
// The tests here are the only ones that can fail for the reason that matters: that
// the projection is wrong about the ABI. Everything upstream — slot arithmetic,
// signature lowering, IID derivation — is checked against metadata, and metadata
// cannot tell you that a call you built from it actually works. These make the
// calls.
package acceptance

import (
	"errors"
	"strings"
	"testing"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/runtime/winui"
	uidispatching "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/dispatching"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlcontrols "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls"
	uixamlprimitives "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls/primitives"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// skipIfUnavailable turns the two environmental failures into skips, and lets
// everything else fail.
//
// Both are facts about the machine, not about the code: the bootstrapper is
// deliberately not committed, and the Windows App SDK runtime is a separate
// install. A test that cannot run should say so rather than report a defect.
func skipIfUnavailable(t *testing.T, err error) {
	t.Helper()
	var dllMissing *winui.ErrBootstrapDLLNotFound
	if errors.As(err, &dllMissing) {
		t.Skipf("bootstrapper not fetched; run `go run ./cmd/generate fetch-bootstrap` (looked in %v)", dllMissing.Searched)
	}
	var noFramework *winui.ErrNoMatchingFrameworkPackage
	if errors.As(err, &noFramework) {
		t.Skipf("no Windows App SDK framework package installed: %v", noFramework)
	}
}

// TestWindowOnScreenWithAGoClickHandler is the whole projection, end to end.
//
// It puts a real WinUI 3 window on screen with a real Button in it, registers a Go
// function as the Click handler, and lets the framework call a different Go function
// back on the UI thread to shut the application down. Every layer is involved:
// composable construction with a null outer, the base-class chain (Content and the
// UIElement view of a Button both come from classes above it), a grounded delegate
// invoked by native code, and a parameterless DispatcherQueueHandler.
//
// It terminates because the enqueued handler calls Application.Exit. Application.Start
// blocks until the message loop ends, and nothing outside the UI thread may end it —
// a cross-apartment call is not an option — so the exit has to come from a callback
// the framework itself invokes. That the test returns at all is therefore part of
// what it proves.
func TestWindowOnScreenWithAGoClickHandler(t *testing.T) {
	var (
		readyRan       bool
		dispatcherRan  bool
		dispatcherOnUI bool
		clickToken     int64
		title          string
	)

	err := app.Run(func(application *uixaml.IApplication) error {
		readyRan = true

		window, err := uixaml.NewWindow()
		if err != nil {
			return err
		}
		if err := window.SetTitle("go-bindings-windowsappsdk acceptance"); err != nil {
			return err
		}
		// Read it back through a separate vtable slot: a get/put pair that agrees is
		// evidence the HSTRING lowering round-trips, which no metadata check covers.
		if title, err = window.Title(); err != nil {
			return err
		}

		button, err := uixamlcontrols.NewButton()
		if err != nil {
			return err
		}

		// Content is on ContentControl, three classes above Button. Reaching it at
		// all is what the base-class projection exists for.
		content, err := button.AsContentControl()
		if err != nil {
			return err
		}
		defer content.Release()
		// Content is typed as IInspectable, so a string has to be boxed the way
		// WinRT boxes one: PropertyValue.CreateString, from go-bindings-winrt.
		propertyValue, err := wrtfoundation.PropertyValueStatics()
		if err != nil {
			return err
		}
		defer propertyValue.Release()
		text, err := propertyValue.CreateString("Click me")
		if err != nil {
			return err
		}
		defer text.Release()
		if err := content.SetContent(text); err != nil {
			return err
		}

		// Click is on Primitives.IButtonBase, two classes above Button.
		base, err := winrt.QueryInterface[uixamlprimitives.IButtonBase](
			unsafe.Pointer(button), &uixamlprimitives.IID_IButtonBase)
		if err != nil {
			return err
		}
		defer base.Release()
		handler, err := uixamlprimitives.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				// Reached only by a real click, which this test cannot synthesize.
				// Registering it is what is being checked; that the framework can
				// call a Go delegate at all is proved below, through the dispatcher.
			})
		if err != nil {
			return err
		}
		defer handler.Close()
		token, err := base.AddClick(handler)
		if err != nil {
			return err
		}
		clickToken = token.Value
		defer func() { _ = base.RemoveClick(token) }()

		// Window.Content takes a UIElement, which Button reaches through the chain.
		element, err := button.AsUIElement()
		if err != nil {
			return err
		}
		defer element.Release()
		if err := window.SetContent(element); err != nil {
			return err
		}
		if err := window.Activate(); err != nil {
			return err
		}

		// The exit has to come from the UI thread, so it is posted to the queue the
		// framework is pumping. DispatcherQueueHandler's Invoke is parameterless,
		// which is why go-bindings-winrt v0.4.0 needed zero-parameter delegates
		// before any of this could work.
		queue, err := window.DispatcherQueue()
		if err != nil {
			return err
		}
		defer queue.Release()
		exit, err := uidispatching.NewDispatcherQueueHandler(func() {
			dispatcherRan = true
			dispatcherOnUI = winrt.CurrentThreadID() == winui.UIThreadID()
			_ = application.Exit()
		})
		if err != nil {
			return err
		}
		// Not closed until the application has exited: the queue holds a reference
		// while the item is pending, and closing early would drop it.
		defer exit.Close()
		enqueued, err := queue.TryEnqueue(exit)
		if err != nil {
			return err
		}
		if !enqueued {
			return errors.New("TryEnqueue returned false; the application would never exit")
		}
		return nil
	}, app.Options{})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}

	if !readyRan {
		t.Error("the initialization callback never ran")
	}
	if title != "go-bindings-windowsappsdk acceptance" {
		t.Errorf("Window.Title round-tripped as %q", title)
	}
	if clickToken == 0 {
		t.Error("AddClick returned a zero registration token")
	}
	if !dispatcherRan {
		t.Error("the enqueued handler never ran, yet the application exited")
	}
	if !dispatcherOnUI {
		t.Error("the enqueued handler did not run on the UI thread; SetInlineThread is not in effect")
	}
}

// TestApplicationResourcesNeedADerivedApplication pins a platform limit, so that
// the day it stops being one is visible.
//
// Application.Resources returns E_UNEXPECTED on an application created the only way
// this module can currently create one: IApplicationFactory.CreateInstance with a
// NULL controlling outer. Everything else on the same interface pointer works, and
// the slot is pinned against the winmd in internal/verify — so what is missing is
// the derived instance the framework would hang application-level state on, and a
// resource dictionary is application-level state.
//
// The test asserts the failure. If a newer Windows App SDK, or the derived-application
// path, makes it succeed, this fails and says so — which is the point. Do not
// "fix" it by loosening the assertion.
func TestApplicationResourcesNeedADerivedApplication(t *testing.T) {
	var resourcesErr error
	err := app.Run(func(application *uixaml.IApplication) error {
		_, resourcesErr = application.Resources()
		return application.Exit()
	}, app.Options{})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if resourcesErr == nil {
		t.Error("Application.Resources now succeeds on a null-outer application: " +
			"XamlControlsResources can be merged by default, and app.Options.ControlsResources " +
			"should become the default rather than an opt-in")
	} else {
		t.Logf("Application.Resources fails as expected without a derived application: %v", resourcesErr)
	}
}

// TestControlsResourcesReportsTheSameLimit checks that the opt-in path surfaces the
// cause rather than a bare HRESULT, since a caller who turns it on is precisely the
// person who needs to know why it did not work.
func TestControlsResourcesReportsTheSameLimit(t *testing.T) {
	err := app.Run(func(application *uixaml.IApplication) error {
		return application.Exit()
	}, app.Options{ControlsResources: true})

	skipIfUnavailable(t, err)
	if err == nil {
		t.Skip("XamlControlsResources merged successfully; see TestApplicationResourcesNeedADerivedApplication")
	}
	if !strings.Contains(err.Error(), "Application.Resources") {
		t.Errorf("error = %v, want it to name the call that failed", err)
	}
}
