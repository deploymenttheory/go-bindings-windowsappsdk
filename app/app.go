//go:build windows && amd64

// Package app is the ergonomic layer over the generated bindings: the startup
// sequence a WinUI application needs, in the one order that works.
//
// Everything here could be written by hand against bindings/winui. It is here
// because getting the order wrong produces symptoms that point somewhere else
// entirely — an E_UNEXPECTED that looks like a broken binding, a message loop that
// hangs instead of reporting an error, a cross-apartment call that looks like a slow
// one. Each of those was hit while writing this, and each is now impossible to hit
// through Run.
package app

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/runtime/winui"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// Options controls startup.
type Options struct {
	// Bootstrap overrides how the Windows App SDK framework package is located.
	// The zero value requests the line this repository targets.
	Bootstrap *winui.BootstrapOptions

	// SkipControlsResources suppresses merging XamlControlsResources into the
	// application's resources.
	//
	// Merging is attempted by default, and currently fails; see
	// AddControlsResources. Nothing depends on it — controls are styled by the
	// framework package regardless — so the failure is reported through
	// OnControlsResourcesError rather than aborting startup.
	SkipControlsResources bool

	// OnControlsResourcesError observes a failed XamlControlsResources merge.
	//
	// It exists because the failure is real but inconsequential: an application
	// runs, and its controls are styled, without those resources. A nil hook
	// ignores it, which is the right default for a caller who cannot act on it,
	// and a diagnostic build can supply one.
	OnControlsResourcesError func(error)
}

// Ready is what Run hands the caller once startup is complete: an application, and
// its first window.
//
// The window is created by Run rather than by the caller because the order matters
// and is not discoverable. Application.Resources is unavailable until the XAML core
// is up, and creating the first Window is what brings it up — so resources can only
// be merged after a window exists. Handing the window over removes the chance of
// getting that wrong.
type Ready struct {
	// Application is Application.Current. It is a borrowed reference owned by the
	// application's own lifetime; do not Release it.
	Application *uixaml.IApplication

	// Window is the application's first window, created but NOT activated: set its
	// content first, then call Activate.
	Window *uixaml.Window
}

// ErrNoApplication reports that Application.Current was nil after the initialization
// callback constructed an application, which should not happen.
var ErrNoApplication = errors.New("app: Application.Current is nil after constructing an Application")

// Run enters the UI thread, starts the application, creates its first window, and
// calls onReady on the UI thread with both.
//
// It blocks until the application exits, which is what Application.Start does — so
// onReady should set the window's content, activate it, and return. Anything that
// needs to happen later belongs in an event handler or on the DispatcherQueue.
//
// Application.Start creates the DispatcherQueueController itself. Do not call
// CreateDispatcherQueueController by hand: a second controller on the same thread is
// an error, and the symptom appears much later.
//
// The error returned from onReady is surfaced here after the application exits, not
// instead of running it: by the time onReady is called the message loop is already
// the thing in charge.
func Run(onReady func(ready *Ready) error, options Options) error {
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
// Go-side derivation is not on the critical path for building a UI at all.
//
// The window is created here rather than by the caller, and that is the ordering
// this function exists to own: Application.Resources is unavailable until the XAML
// core is up, and the first Window is what brings it up. Merging resources before
// then fails with E_UNEXPECTED, which reads as a defect and is not one.
func initialize(statics *uixaml.IApplicationStatics, onReady func(*Ready) error, options Options) error {
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

	window, err := uixaml.NewWindow()
	if err != nil {
		return fmt.Errorf("app: creating the first Window: %w", err)
	}

	// Only now are resources reachable. A failure here is reported, not fatal: the
	// application still runs, with unstyled controls.
	if !options.SkipControlsResources {
		if err := AddControlsResources(application); err != nil && options.OnControlsResourcesError != nil {
			options.OnControlsResourcesError(err)
		}
	}
	if onReady == nil {
		return nil
	}
	return onReady(&Ready{Application: application, Window: window})
}

// AddControlsResources merges XamlControlsResources into the application's
// resources.
//
// Why it matters: without those resources controls render unstyled and every
// {ThemeResource} lookup fails while parsing, which is what "XamlReader.Load does
// not work in WinUI 3" almost always turns out to be — a resources problem wearing a
// parser's clothes.
//
// Ordering. Application.Resources is unavailable until the XAML core is up, and
// creating the first Window is what brings it up. Called before then it returns
// E_UNEXPECTED (0x8000FFFF), which reads like a defect and is not one — it is the
// framework saying "too early". Run creates the window first for exactly this
// reason; a caller invoking this directly must do the same.
// TestResourcesRequireTheXamlCore pins both halves of that transition.
//
// What does not work, and what that turned out not to mean. Activating
// XamlControlsResources returns E_FAIL, at every point tried. The cause is still
// unknown, but a spike established what it is NOT: a plain ResourceDictionary and
// ordinary controls activate fine, and XamlReader resolves and instantiates framework
// types from markup, so neither the projection nor the absence of an
// application-provided IXamlMetadataProvider explains it.
//
// It also turns out not to matter. WinUI 3 ships its default styles in the framework
// package — unlike WinUI 2, where they came from a NuGet library and had to be merged
// — so controls are templated and sized without it.
// TestControlsAreStyledWithoutXamlControlsResources measures a Button at Loaded and
// finds both. XamlControlsResources' remaining job in WinUI 3 is UseCompactResources.
//
// So this is a gap worth noting, not a blocker. Merging is attempted by default
// because it is the right thing to want; the failure is reported through
// Options.OnControlsResourcesError and nothing depends on it succeeding.
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

	controlsResources, err := uixaml.NewXamlControlsResources()
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
