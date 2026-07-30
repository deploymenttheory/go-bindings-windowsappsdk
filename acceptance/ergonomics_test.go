//go:build windows && amd64

package acceptance

import (
	"testing"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// TestBoxRoundTripsThroughWinRT checks that a boxed value is the property value WinRT
// thinks it is, by reading it back through IPropertyValue rather than by trusting the
// Create call that made it.
//
// Boxing is where a plausible-looking helper goes wrong quietly: CreateInt32 and
// CreateInt64 both succeed on any integer, and the difference only shows up later, at
// whatever tries to unbox. So the Type is asserted, not just the value.
func TestBoxRoundTripsThroughWinRT(t *testing.T) {
	enterApartment(t)

	// Deliberately not t.Run: a subtest runs on its own goroutine, and so on a thread
	// where enterApartment's apartment does not exist. See apartment_test.go.
	for _, testCase := range []struct {
		name     string
		value    any
		wantType wrtfoundation.PropertyType
		read     func(*wrtfoundation.IPropertyValue) (any, error)
	}{
		{"string", "hello", wrtfoundation.PropertyTypeString,
			func(p *wrtfoundation.IPropertyValue) (any, error) { v, err := p.GetString(); return v, err }},
		{"bool", true, wrtfoundation.PropertyTypeBoolean,
			func(p *wrtfoundation.IPropertyValue) (any, error) { v, err := p.GetBoolean(); return v, err }},
		{"float64", 2.5, wrtfoundation.PropertyTypeDouble,
			func(p *wrtfoundation.IPropertyValue) (any, error) { v, err := p.GetDouble(); return v, err }},
		// A Go int must box as an Int32 while it fits: XAML's own converters expect the
		// narrower type, and an Int64 fails at the unbox rather than at the call.
		{"int", 42, wrtfoundation.PropertyTypeInt32,
			func(p *wrtfoundation.IPropertyValue) (any, error) { v, err := p.GetInt32(); return int(v), err }},
	} {
		boxed, err := app.Box(testCase.value)
		if err != nil {
			t.Errorf("%s: Box(%v): %v", testCase.name, testCase.value, err)
			continue
		}

		property, err := winrt.QueryInterface[wrtfoundation.IPropertyValue](
			unsafe.Pointer(boxed), &wrtfoundation.IID_IPropertyValue)
		if err != nil {
			t.Errorf("%s: the boxed value is not an IPropertyValue: %v", testCase.name, err)
			boxed.Release()
			continue
		}

		if kind, err := property.Type(); err != nil {
			t.Errorf("%s: Type: %v", testCase.name, err)
		} else if kind != testCase.wantType {
			t.Errorf("%s: boxed as %v, want %v", testCase.name, kind, testCase.wantType)
		}
		if got, err := testCase.read(property); err != nil {
			t.Errorf("%s: reading it back: %v", testCase.name, err)
		} else if got != testCase.value {
			t.Errorf("%s: round-tripped to %#v, want %#v", testCase.name, got, testCase.value)
		}

		property.Release()
		boxed.Release()
	}
}

// TestErgonomicHelpersBuildALiveTree drives the whole layer against a real window: a
// panel whose properties are set as a block, an event registered through On, children
// appended through Append, and a Button whose Content is a boxed string.
//
// The assertion is that it RENDERS — measured at Loaded, the earliest point at which a
// size means anything. Every helper here hides a reference-counting step, and the way
// those fail is not an error return: it is a control that never appears, or a process
// that outlives its objects.
func TestErgonomicHelpersBuildALiveTree(t *testing.T) {
	var (
		loadedFired bool
		panelWidth  float64
		clicked     bool
		buildErr    error
	)

	err := app.Run(func(ready *app.Ready) error {
		panel, err := uixaml.NewStackPanel()
		if err != nil {
			return err
		}
		label, err := uixaml.NewTextBlock()
		if err != nil {
			return err
		}
		button, err := uixaml.NewButton()
		if err != nil {
			return err
		}

		// A block of setters, including one that boxes.
		if buildErr = app.All(
			panel.SetSpacing(8),
			label.SetText("ergonomics"),
			app.SetContent(button.AsContentControl, "Press"),
		); buildErr != nil {
			return buildErr
		}

		// An event through On, composed with With — Click is declared two classes
		// above Button. Not clicked by this test, since there is no input to simulate,
		// but registering it exercises the grounding and the token, and a failure to
		// register is an error rather than a silent no-op.
		if buildErr = app.With(button.AsButtonBase, func(base *uixaml.IButtonBase) error {
			_, err := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { clicked = true })
			return err
		}); buildErr != nil {
			return buildErr
		}

		if buildErr = app.Append(panel.AsPanel, label.AsUIElement, button.AsUIElement); buildErr != nil {
			return buildErr
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
		if err := app.With(panel.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			_, addErr := frame.AddLoaded(loaded)
			return addErr
		}); err != nil {
			return err
		}

		if err := app.With(panel.AsUIElement, ready.Window.SetContent); err != nil {
			return err
		}
		return ready.Window.Activate()
	}, app.Options{})

	skipIfUnavailable(t, err)
	if err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if buildErr != nil {
		t.Fatalf("building the tree through the ergonomic helpers: %v", buildErr)
	}
	if !loadedFired {
		t.Fatal("the panel's Loaded event never fired, so nothing was measured")
	}
	if panelWidth <= 0 {
		t.Errorf("the panel measures %.1f wide: the tree built through the helpers "+
			"never rendered", panelWidth)
	} else {
		t.Logf("a tree built entirely through app.All/With/On/Append/SetContent "+
			"rendered at %.1f wide", panelWidth)
	}
	_ = clicked
}
