//go:build windows && amd64

package app

// Why a binding that resolves nothing has to be asked about.
//
// A XAML binding NEVER THROWS. When it cannot resolve a path it writes a line to the
// debug output and leaves the target property at its default — so a TextBlock whose
// Text is bound to a name the engine cannot find renders empty, and every other
// signal a test can reach says the page is fine. The element is in the tree, it has
// a template, it measures, Loaded fires. Only the pixels are missing.
//
// That is not hypothetical here. examples/gallery's AnnotatedScrollBarSummaryPage
// binds {Binding Content} in its LabelTemplate, advertises eight labels, and draws
// none of them, while acceptance/gallery_test.go passes it green.
//
// DebugSettings.BindingFailed is the event that says why, and nothing in this module
// used to subscribe to it. Wiring it converts the one failure mode that is silent by
// design into a message a test can fail on.
//
// # WHAT IT DOES NOT CATCH, MEASURED
//
// It did not fire for the case above. AnnotatedScrollBarSummaryPage was built with
// this hook installed and TraceBindings on, and reported NOTHING, while the labels it
// binds still rendered blank. So a quiet hook does NOT mean every binding resolved.
//
// Two readings survive that observation, and it does not separate them: the event may
// need a debugger attached to be raised at all, or the failing template's binding is
// never evaluated — in which case there is no binding failure to report and the fault
// is in how the template is applied. Either way, the conclusion for a caller is the
// same: treat a message as proof a binding failed, never treat silence as proof one
// did not.
//
// It is kept because a positive is still worth having and costs nothing, and because
// the alternative — assuming it works — is what made the gap invisible in the first
// place.

import (
	"fmt"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// watchBindings subscribes to DebugSettings.BindingFailed and, when tracing is asked
// for, turns on the tracing that makes the framework raise it for every failure
// rather than only the ones it reports by default.
//
// It is called from initialize with the application Current returned, before the
// first Window exists: a binding cannot fail before there is content to bind, so
// subscribing this early cannot miss one.
//
// The handler is deliberately not closed and the token is dropped. The subscription
// lasts as long as the application does, which is the life of the message loop —
// there is no later moment at which unsubscribing would be correct, and holding the
// token would only invite it.
func watchBindings(application *uixaml.IApplication, options Options) error {
	if options.OnBindingFailed == nil && !options.TraceBindings {
		return nil
	}

	settings, err := application.DebugSettings()
	if err != nil {
		return fmt.Errorf("app: Application.DebugSettings: %w", err)
	}
	defer settings.Release()

	// Tracing on its own writes to the debugger's output window, which a test
	// cannot read. It is still worth setting: it widens what the framework
	// considers worth raising, and the handler below is what makes it observable.
	if options.TraceBindings {
		if err := settings.SetIsBindingTracingEnabled(true); err != nil {
			return fmt.Errorf("app: enabling binding tracing: %w", err)
		}
	}

	if options.OnBindingFailed == nil {
		return nil
	}

	report := options.OnBindingFailed
	if _, err := On(settings.AddBindingFailed, uixaml.NewBindingFailedEventHandler,
		func(_ *syswinrt.IInspectable, args *uixaml.IBindingFailedEventArgs) {
			message, err := args.Message()
			if err != nil {
				// The event fired, so a binding did fail; losing the text is
				// worth saying rather than swallowing.
				report("app: a binding failed and its message could not be read: " + err.Error())
				return
			}
			report(message)
		}); err != nil {
		return fmt.Errorf("app: subscribing to BindingFailed: %w", err)
	}
	return nil
}
