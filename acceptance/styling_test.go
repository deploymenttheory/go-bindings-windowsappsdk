//go:build windows && amd64

package acceptance

import (
	"testing"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlcontrols "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls"
	uixamlmarkup "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/markup"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// These tests exist because a previous version of this package asserted the opposite
// of each of them, on the strength of an untested explanation. They are the
// corrections, kept as tests so the record cannot drift back.

// TestControlsAreStyledWithoutXamlControlsResources is the one that matters.
//
// XamlControlsResources cannot be activated from Go — see
// TestXamlControlsResourcesCannotBeActivated — and I concluded from that, plus a
// Button measuring 0x0, that controls render unstyled and the fix needed COM
// aggregation. Both halves were wrong. The 0x0 was measured before layout had run.
//
// Measured at Loaded, which is the earliest point at which a size means anything —
// the element is in the live tree with its template applied — a Button has a real
// template and a real size. WinUI 3 ships its default styles in the framework
// package, unlike WinUI 2 where they came from a NuGet library and had to be merged;
// XamlControlsResources is a compatibility shim, not a prerequisite.
func TestControlsAreStyledWithoutXamlControlsResources(t *testing.T) {
	var (
		loadedFired   bool
		hasTemplate   bool
		width, height float64
	)

	err := app.Run(func(ready *app.Ready) error {
		button, err := uixamlcontrols.NewButton()
		if err != nil {
			return err
		}
		frameworkElement, err := button.AsFrameworkElement()
		if err != nil {
			return err
		}
		defer frameworkElement.Release()

		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				loadedFired = true
				if control, err := button.AsControl(); err == nil {
					template, _ := control.Template()
					hasTemplate = template != nil
					if template != nil {
						template.Release()
					}
					control.Release()
				}
				if element, err := button.AsFrameworkElement(); err == nil {
					width, _ = element.ActualWidth()
					height, _ = element.ActualHeight()
					element.Release()
				}
				_ = ready.Application.Exit()
			})
		if err != nil {
			return err
		}
		defer loaded.Close()
		if _, err := frameworkElement.AddLoaded(loaded); err != nil {
			return err
		}

		element, err := button.AsUIElement()
		if err != nil {
			return err
		}
		defer element.Release()
		if err := ready.Window.SetContent(element); err != nil {
			return err
		}
		return ready.Window.Activate()
	}, app.Options{})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if !loadedFired {
		t.Fatal("the Button's Loaded event never fired, so nothing was measured")
	}
	if !hasTemplate {
		t.Error("the Button has no Template at Loaded: controls really are unstyled, and " +
			"XamlControlsResources is a prerequisite after all")
	}
	if width <= 0 || height <= 0 {
		t.Errorf("the Button measures %.1fx%.1f at Loaded, so it has no visual", width, height)
	} else {
		t.Logf("Button is templated and measures %.1fx%.1f — styled by the framework", width, height)
	}
}

// TestXamlControlsResourcesCannotBeActivated pins the one thing that genuinely does
// not work, now stripped of the explanation it never earned.
//
// Activating it returns E_FAIL at every point tried. What the spike established is
// what it is NOT: not a broken projection, and not a missing metadata provider.
// Neighbouring types activate fine and XAML type resolution works — see
// TestXamlTypeResolutionWorksWithoutAMetadataProvider — so the cause is specific to
// this type and remains unknown.
//
// It also does not matter, which is the useful part: controls are styled without it.
// Its remaining purpose in WinUI 3 is UseCompactResources, so this is a gap to note
// rather than a blocker to clear.
func TestXamlControlsResourcesCannotBeActivated(t *testing.T) {
	var (
		resourcesErr  error
		dictionaryErr error
		panelErr      error
	)

	err := app.Run(func(ready *app.Ready) error {
		// The controls: an ordinary [Activatable] XAML type, and the base class
		// XamlControlsResources derives from. Both must work, or the failure is not
		// specific to XamlControlsResources.
		dictionary, dictionaryErr2 := uixaml.NewResourceDictionary()
		dictionaryErr = dictionaryErr2
		if dictionary != nil {
			dictionary.Release()
		}
		panel, panelErr2 := uixamlcontrols.NewStackPanel()
		panelErr = panelErr2
		if panel != nil {
			panel.Release()
		}

		resources, resourcesErr2 := uixamlcontrols.NewXamlControlsResources()
		resourcesErr = resourcesErr2
		if resources != nil {
			resources.Release()
		}
		return ready.Application.Exit()
	}, app.Options{SkipControlsResources: true})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if dictionaryErr != nil {
		t.Errorf("a plain ResourceDictionary failed to activate (%v) — the problem is not "+
			"specific to XamlControlsResources after all", dictionaryErr)
	}
	if panelErr != nil {
		t.Errorf("an ordinary control failed to activate (%v) — XAML activation is broken "+
			"generally, which is a much larger problem than this test describes", panelErr)
	}
	if resourcesErr == nil {
		t.Error("XamlControlsResources now activates: remove this test and make " +
			"app.Options merge it by default")
	} else {
		t.Logf("XamlControlsResources still fails to activate: %v", resourcesErr)
	}
}

// TestXamlTypeResolutionWorksWithoutAMetadataProvider is the discriminator that killed
// the aggregation theory.
//
// The theory was: a Go application cannot answer QueryInterface for
// IXamlMetadataProvider, therefore XAML types cannot be resolved by name, therefore
// XamlControlsResources fails. The first link is true and the second is false —
// XamlReader parses markup and instantiates framework types perfectly well, because
// the framework resolves its own types and never needed us to.
//
// So COM aggregation is not on the critical path for a working UI, and building it to
// fix styling would have fixed nothing. A provider is still what a Go application
// would need to resolve types IT defines, which is a real future concern and a
// different one.
func TestXamlTypeResolutionWorksWithoutAMetadataProvider(t *testing.T) {
	const namespace = `xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"`
	type probe struct {
		label     string
		xaml      string
		wantError bool
	}
	probes := []probe{
		{"a framework type", `<Grid ` + namespace + `/>`, false},
		{"a type with a property set", `<TextBlock ` + namespace + ` Text="hi"/>`, false},
		{"a resource dictionary", `<ResourceDictionary ` + namespace + `/>`, false},
		// The control: an unknown type must FAIL, or the parser is not really
		// resolving anything and the successes above prove nothing.
		{"an unknown type", `<NotARealType ` + namespace + `/>`, true},
	}
	results := make([]error, len(probes))
	var providerErr error

	err := app.Run(func(ready *app.Ready) error {
		_, providerErr = winrt.QueryInterface[uixamlmarkup.IXamlMetadataProvider](
			unsafe.Pointer(ready.Application), &uixamlmarkup.IID_IXamlMetadataProvider)

		reader, err := uixamlmarkup.XamlReaderStatics()
		if err != nil {
			return err
		}
		defer reader.Release()
		for i := range probes {
			loaded, err := reader.Load(probes[i].xaml)
			results[i] = err
			if loaded != nil {
				loaded.Release()
			}
		}
		return ready.Application.Exit()
	}, app.Options{SkipControlsResources: true})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if providerErr == nil {
		t.Log("the Application now answers IXamlMetadataProvider; this test's premise " +
			"(that it does not, and that this does not matter) needs revisiting")
	}
	for i, p := range probes {
		switch {
		case p.wantError && results[i] == nil:
			t.Errorf("XamlReader.Load accepted %s, so the successes above prove nothing "+
				"about type resolution", p.label)
		case !p.wantError && results[i] != nil:
			t.Errorf("XamlReader.Load failed on %s: %v — XAML type resolution does need "+
				"an application-provided metadata provider after all", p.label, results[i])
		}
	}
}
