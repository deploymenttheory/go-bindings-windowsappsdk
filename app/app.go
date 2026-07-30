//go:build windows && amd64

// Package app is the ergonomic layer over the generated bindings: the startup
// sequence a WinUI application needs, in the one order that works, with the two
// traps that are not discoverable from the API surface already handled.
//
// Everything here could be written by hand against bindings/winui. It is here
// because getting it wrong produces symptoms that point somewhere else entirely —
// an unstyled window, a parser error inside XamlReader, a cross-apartment call that
// looks like a slow one.
package app

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/runtime/winui"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlcontrols "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// Options controls startup.
type Options struct {
	// Bootstrap overrides how the Windows App SDK framework package is located.
	// The zero value requests the line this repository targets.
	Bootstrap *winui.BootstrapOptions

	// ControlsResources asks for XamlControlsResources to be merged into the
	// application's resources.
	//
	// It is opt-in, and it currently FAILS — see AddControlsResources. It is
	// off by default because the failure is not recoverable from here and a
	// library should not begin by returning an error nobody can act on.
	//
	// Turning it on is how you find out whether a newer Windows App SDK, or the
	// derived-application path, has changed that.
	ControlsResources bool
}

// ErrNoApplication reports that Application.Current was nil inside the
// initialization callback, which should not happen: Application.Start creates the
// application before invoking it.
var ErrNoApplication = errors.New("app: Application.Current is nil inside the initialization callback")

// Run enters the UI thread, starts the application, and calls onReady on the UI
// thread once the application exists and its resources are in place.
//
// It blocks until the application exits, which is what Application.Start does — so
// onReady should create a window, activate it, and return. Anything that needs to
// happen later belongs in an event handler or on the DispatcherQueue.
//
// Application.Start creates the DispatcherQueueController itself. Do not call
// CreateDispatcherQueueController by hand: a second controller on the same thread
// is an error, and the symptom appears much later.
//
// The error returned from onReady is surfaced here after the application exits, not
// instead of running it: by the time onReady is called the message loop is already
// the thing in charge.
func Run(onReady func(application *uixaml.IApplication) error, options Options) error {
	bootstrap := winui.DefaultBootstrap()
	if options.Bootstrap != nil {
		bootstrap = *options.Bootstrap
	}
	release, err := winui.EnterUIThreadWith(bootstrap)
	if err != nil {
		return err
	}
	defer release()

	statics, err := uixaml.ApplicationStatics()
	if err != nil {
		return fmt.Errorf("app: Application statics: %w", err)
	}
	defer statics.Release()

	// readyErr is written on the UI thread inside the callback and read here after
	// Start returns. No synchronisation is needed and none would help: Start does
	// not return until the message loop has ended, which is strictly after every
	// callback it ran.
	var readyErr error
	callback, err := uixaml.NewApplicationInitializationCallback(
		func(_ *uixaml.IApplicationInitializationCallbackParams) {
			readyErr = initialize(statics, onReady, options)
			if readyErr == nil {
				return
			}
			// A failed startup must not leave Start pumping an empty message loop
			// for the life of the process. This callback's thread is the only one
			// that may end it — a cross-apartment Exit is not an option — so the
			// shutdown has to happen here, before returning into the framework.
			if application, currentErr := statics.Current(); currentErr == nil && application != nil {
				_ = application.Exit()
			}
		})
	if err != nil {
		return fmt.Errorf("app: building the initialization callback: %w", err)
	}
	defer callback.Close()

	if err := statics.Start(callback); err != nil {
		return fmt.Errorf("app: Application.Start: %w", err)
	}
	return readyErr
}

// initialize runs inside the initialization callback, on the UI thread.
//
// The callback's contract is easy to misread: Application.Start creates the
// DispatcherQueueController and then calls back, but it does NOT create the
// application. Constructing one is the callback's job — that is what every
// `Application.Start(_ => new App())` in the C# and C++ samples is doing — and
// without it Application.Current stays nil and Start runs a message loop over
// nothing, forever, with no error to explain why.
//
// It does not have to be a DERIVED application, which is the useful discovery here:
// a plain Microsoft.UI.Xaml.Application is enough to get a working Current, so
// Go-side derivation (which would need COM aggregation support that does not exist)
// is not on the critical path for building a UI at all.
func initialize(statics *uixaml.IApplicationStatics, onReady func(*uixaml.IApplication) error, options Options) error {
	created, err := uixaml.NewApplication()
	if err != nil {
		return fmt.Errorf("app: constructing the Application: %w", err)
	}
	// Not released: the application owns itself for the life of the message loop,
	// and dropping this reference inside the callback would destroy it before Start
	// had anything to run.
	_ = created

	application, err := statics.Current()
	if err != nil {
		return fmt.Errorf("app: Application.Current: %w", err)
	}
	if application == nil {
		return ErrNoApplication
	}
	// Current is a borrowed reference from the application's own lifetime, not a new
	// one to release: releasing it here would drop the application's count.

	if options.ControlsResources {
		if err := AddControlsResources(application); err != nil {
			return err
		}
	}
	if onReady == nil {
		return nil
	}
	return onReady(application)
}

// AddControlsResources merges XamlControlsResources into the application's
// resources.
//
// Why it matters: without those resources controls render unstyled and every
// {ThemeResource} lookup fails while parsing, which is what "XamlReader.Load does
// not work in WinUI 3" almost always turns out to be — a resources problem wearing a
// parser's clothes.
//
// Why it does not work yet. Against a Windows App SDK 2.3 runtime this returns
// E_UNEXPECTED (0x8000FFFF) from the very first call, Application.Resources, on an
// application created the only way this module can currently create one: through
// IApplicationFactory.CreateInstance with a NULL controlling outer.
//
// That is not a projection bug, and it is worth being precise about why, because
// "catastrophic failure" invites the assumption that it is:
//
//   - get_Resources is slot 6 on IApplication, which internal/verify pins against
//     the committed winmd, and the generated call dispatches slot 6.
//   - Every other call on the same interface pointer works — Exit ends the message
//     loop, and Current returned this pointer in the first place.
//   - Window, Button, content, activation, the DispatcherQueue and a Go delegate
//     invoked by the framework all work on the same application.
//
// What is left is the composition shape. Application is designed to be derived
// from: the outer object is the application, and the inner is its base. Created
// with a null outer there is no derived instance for the framework to hang
// application-level state on, and the resource dictionary is application-level
// state. Deriving needs COM aggregation support that go-bindings-winrt does not
// have yet — which is exactly the milestone this repository already has for it.
//
// TestApplicationResourcesNeedADerivedApplication pins the failure so that a newer
// SDK, or the derived path landing, shows up as a test that has started passing.
func AddControlsResources(application *uixaml.IApplication) error {
	resources, err := application.Resources()
	if err != nil {
		return fmt.Errorf("app: Application.Resources: %w", err)
	}
	defer resources.Release()

	merged, err := resources.MergedDictionaries()
	if err != nil {
		return fmt.Errorf("app: Resources.MergedDictionaries: %w", err)
	}
	defer merged.Release()

	controlsResources, err := uixamlcontrols.NewXamlControlsResources()
	if err != nil {
		return fmt.Errorf("app: XamlControlsResources: %w", err)
	}
	defer controlsResources.Release()

	// The dictionary holds IResourceDictionary, and XamlControlsResources reaches it
	// through the base-class chain: XamlControlsResources extends ResourceDictionary.
	dictionary, err := winrt.QueryInterface[uixaml.IResourceDictionary](
		unsafe.Pointer(controlsResources), &uixaml.IID_IResourceDictionary)
	if err != nil {
		return fmt.Errorf("app: XamlControlsResources as IResourceDictionary: %w", err)
	}
	defer dictionary.Release()

	if err := merged.Append(dictionary); err != nil {
		return fmt.Errorf("app: appending XamlControlsResources: %w", err)
	}
	return nil
}
