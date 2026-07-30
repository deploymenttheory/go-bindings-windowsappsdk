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
	"testing"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/runtime/winui"
	uidispatching "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/dispatching"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
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
//
// The Button really renders: TestControlsAreStyledWithoutXamlControlsResources
// measures it at Loaded and finds a template and a size.
func TestWindowOnScreenWithAGoClickHandler(t *testing.T) {
	var (
		readyRan       bool
		dispatcherRan  bool
		dispatcherOnUI bool
		clickToken     int64
		title          string
	)

	err := app.Run(func(ready *app.Ready) error {
		readyRan = true
		window := ready.Window

		if err := window.SetTitle("go-bindings-windowsappsdk acceptance"); err != nil {
			return err
		}
		// Read it back through a separate vtable slot: a get/put pair that agrees is
		// evidence the HSTRING lowering round-trips, which no metadata check covers.
		var err error
		if title, err = window.Title(); err != nil {
			return err
		}

		button, err := uixaml.NewButton()
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

		// Click is on Primitives.IButtonBase, two classes above Button. Controls and
		// Controls.Primitives reference each other, so one import direction is
		// severed and there is no generated AsButtonBase — but a consuming package
		// closes no cycle, so the generic QueryInterface reaches it.
		base, err := winrt.QueryInterface[uixaml.IButtonBase](
			unsafe.Pointer(button), &uixaml.IID_IButtonBase)
		if err != nil {
			return err
		}
		defer base.Release()
		handler, err := uixaml.NewRoutedEventHandler(
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
			_ = ready.Application.Exit()
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

// TestResourcesRequireTheXamlCore is the evidence for why app.Run creates the window
// itself, and it replaces an earlier test that asserted the wrong cause.
//
// Application.Resources is unavailable until the XAML core is up, and creating the
// first Window is what brings it up. Called before then it returns E_UNEXPECTED,
// which reads like a broken binding and is not one.
//
// Both halves are asserted, because only the TRANSITION distinguishes "too early"
// from "broken". A test that observed the failure alone would have supported any
// explanation at all — which is exactly how the wrong one came to be written down.
//
// It drives the raw startup path rather than app.Run, because app.Run deliberately
// makes the "before" state unreachable. Adding a hook to Options just to observe it
// would be test-only API weakening the guarantee being tested.
func TestResourcesRequireTheXamlCore(t *testing.T) {
	release, err := winui.EnterUIThread()
	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("EnterUIThread: %v", err)
	}
	defer release()

	statics, err := uixaml.ApplicationStatics()
	if err != nil {
		t.Fatalf("ApplicationStatics: %v", err)
	}
	defer statics.Release()

	var beforeErr, afterErr error
	var ran bool
	callback, err := uixaml.NewApplicationInitializationCallback(
		func(_ *uixaml.IApplicationInitializationCallbackParams) {
			ran = true
			if _, err := uixaml.NewApplication(); err != nil {
				t.Errorf("NewApplication: %v", err)
				return
			}
			application, err := statics.Current()
			if err != nil || application == nil {
				t.Errorf("Application.Current: %v", err)
				return
			}

			_, beforeErr = application.Resources()

			if _, err := uixaml.NewWindow(); err != nil {
				t.Errorf("NewWindow: %v", err)
			}
			_, afterErr = application.Resources()

			_ = application.Exit()
		})
	if err != nil {
		t.Fatalf("NewApplicationInitializationCallback: %v", err)
	}
	defer callback.Close()

	if err := statics.Start(callback); err != nil {
		t.Fatalf("Application.Start: %v", err)
	}
	if !ran {
		t.Fatal("the initialization callback never ran")
	}
	if beforeErr == nil {
		t.Error("Application.Resources succeeded BEFORE the first Window: the ordering " +
			"constraint app.Run is built around no longer holds, and Run could be simplified")
	}
	if afterErr != nil {
		t.Errorf("Application.Resources failed AFTER the first Window: %v", afterErr)
	}
}

// TestRunSequencesResourcesAfterTheWindow is the other half: that app.Run actually
// satisfies the constraint above. By the time onReady is called, Resources works.
func TestRunSequencesResourcesAfterTheWindow(t *testing.T) {
	var resourcesErr error
	err := app.Run(func(ready *app.Ready) error {
		if ready.Window == nil {
			return errors.New("Run did not create a Window")
		}
		_, resourcesErr = ready.Application.Resources()
		return ready.Application.Exit()
	}, app.Options{})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if resourcesErr != nil {
		t.Errorf("Application.Resources failed inside onReady: %v — Run's ordering is wrong", resourcesErr)
	}
}
