//go:build windows && amd64

package acceptance

import (
	"testing"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// TestTypedCollectionDrivesTheLiveVisualTree is the live check on monomorphized
// generic instantiations, and it is a live check because the failure mode is invisible
// at compile time.
//
// Panel.Children returns UIElementCollection, whose only interface is
// IVector`1<UIElement>. The emitter grounds that into a package-local
// IVectorOfUIElement whose methods dispatch through vtable slots computed from the
// OPEN interface's MethodDef order, with the type arguments substituted in. Nothing
// about that is checkable by the Go compiler: if the slots were off by one, Append
// would call GetAt and the call would still type-check.
//
// So this drives the real collection: append two elements, read Size back, index one
// out with GetAt, and confirm the children actually rendered by measuring the parent.
func TestTypedCollectionDrivesTheLiveVisualTree(t *testing.T) {
	var (
		loadedFired   bool
		size          uint32
		indexedIsSame bool
		panelWidth    float64
		appendErr     error
	)

	err := app.Run(func(ready *app.Ready) error {
		panel, err := uixaml.NewStackPanel()
		if err != nil {
			return err
		}
		asPanel, err := panel.AsPanel()
		if err != nil {
			return err
		}
		defer asPanel.Release()

		// The typed collection, with no QueryInterface: the property's own Go type is
		// the instantiation, because a class reference is its default interface at the
		// ABI and this class's default IS IVector<UIElement>.
		children, err := asPanel.Children()
		if err != nil {
			return err
		}
		defer children.Release()

		first, err := uixaml.NewTextBlock()
		if err != nil {
			return err
		}
		if err := first.SetText("first"); err != nil {
			return err
		}
		second, err := uixaml.NewButton()
		if err != nil {
			return err
		}

		firstElement, err := first.AsUIElement()
		if err != nil {
			return err
		}
		defer firstElement.Release()
		secondElement, err := second.AsUIElement()
		if err != nil {
			return err
		}
		defer secondElement.Release()

		if appendErr = children.Append(firstElement); appendErr != nil {
			return appendErr
		}
		if appendErr = children.Append(secondElement); appendErr != nil {
			return appendErr
		}
		size, _ = children.Size()

		// GetAt is a different slot from Append, so a slot error shows up as the wrong
		// element coming back rather than as a failure.
		if got, err := children.GetAt(0); err == nil && got != nil {
			// Same COM object, so the same interface pointer for the same interface.
			indexedIsSame = got == firstElement
			got.Release()
		}

		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				loadedFired = true
				if element, err := panel.AsFrameworkElement(); err == nil {
					panelWidth, _ = element.ActualWidth()
					element.Release()
				}
				_ = ready.Application.Exit()
			})
		if err != nil {
			return err
		}
		defer loaded.Close()
		if frame, err := panel.AsFrameworkElement(); err == nil {
			defer frame.Release()
			if _, err := frame.AddLoaded(loaded); err != nil {
				return err
			}
		}

		root, err := panel.AsUIElement()
		if err != nil {
			return err
		}
		defer root.Release()
		if err := ready.Window.SetContent(root); err != nil {
			return err
		}
		return ready.Window.Activate()
	}, app.Options{})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if appendErr != nil {
		t.Fatalf("Append through the monomorphized IVector<UIElement>: %v", appendErr)
	}
	if size != 2 {
		t.Errorf("Children.Size() is %d after two Appends, want 2 — the monomorphized "+
			"slots do not line up with the open interface's MethodDef order", size)
	}
	if !indexedIsSame {
		t.Error("Children.GetAt(0) did not return the element appended first")
	}
	if !loadedFired {
		t.Fatal("the panel's Loaded event never fired, so nothing was measured")
	}
	if panelWidth <= 0 {
		t.Errorf("the panel measures %.1f wide, so the appended children never rendered", panelWidth)
	} else {
		t.Logf("two children appended through the typed collection, panel measures %.1f wide", panelWidth)
	}
}
