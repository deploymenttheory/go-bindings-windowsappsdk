//go:build windows && amd64

// Command clicker is a WinUI 3 application in about a hundred lines of Go.
//
// It puts a real window on screen with a StackPanel, a TextBlock and a Button, and
// the Button's Click handler is a Go closure that updates the TextBlock. Nothing here
// is a test harness: it runs until you close the window.
//
//	go run ./examples/clicker
//
// It needs the redistributable bootstrapper, which is not committed:
//
//	go run ./cmd/generate fetch-bootstrap
//
// and the Windows App SDK 2.x runtime installed on the machine.
package main

import (
	"fmt"
	"os"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlcontrols "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls"
	uixamlprimitives "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls/primitives"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

func main() {
	if err := app.Run(build, app.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, "clicker:", err)
		os.Exit(1)
	}
}

// build assembles the window's content. It runs on the UI thread, and returning does
// not end the application — app.Run blocks in the framework's message loop until the
// window closes.
func build(ready *app.Ready) error {
	if err := ready.Window.SetTitle("Clicker — WinUI 3 from Go"); err != nil {
		return err
	}

	panel, err := uixamlcontrols.NewStackPanel()
	if err != nil {
		return err
	}
	if err := panel.SetSpacing(16); err != nil {
		return err
	}
	if err := panel.SetPadding(uixaml.Thickness{Left: 32, Top: 32, Right: 32, Bottom: 32}); err != nil {
		return err
	}
	// Centre the stack in the window rather than letting it stretch to the corners.
	if frame, err := panel.AsFrameworkElement(); err == nil {
		defer frame.Release()
		_ = frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentCenter)
		_ = frame.SetVerticalAlignment(uixaml.VerticalAlignmentCenter)
	}

	label, err := uixamlcontrols.NewTextBlock()
	if err != nil {
		return err
	}
	if err := label.SetText("Nothing clicked yet."); err != nil {
		return err
	}

	button, err := uixamlcontrols.NewButton()
	if err != nil {
		return err
	}
	if err := setButtonText(button, "Click me"); err != nil {
		return err
	}

	// The handler is an ordinary Go closure. It runs on the UI thread — the framework
	// invokes it there and go-bindings-winrt's inline-thread mode keeps it there — so
	// it may touch XAML objects directly.
	clicks := 0
	handler, err := uixamlprimitives.NewRoutedEventHandler(
		func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
			clicks++
			word := "times"
			if clicks == 1 {
				word = "time"
			}
			_ = label.SetText(fmt.Sprintf("Clicked %d %s.", clicks, word))
		})
	if err != nil {
		return err
	}
	// Not closed: the handler stays registered for the life of the window.

	// Click is declared on Primitives.IButtonBase, two classes above Button. Controls
	// and Controls.Primitives reference each other, so one import direction is severed
	// and there is no generated AsButtonBase — a consuming package like this one closes
	// no cycle, so the generic QueryInterface reaches it.
	base, err := winrt.QueryInterface[uixamlprimitives.IButtonBase](
		unsafe.Pointer(button), &uixamlprimitives.IID_IButtonBase)
	if err != nil {
		return err
	}
	defer base.Release()
	if _, err := base.AddClick(handler); err != nil {
		return err
	}

	if err := addChildren(panel, label, button); err != nil {
		return err
	}

	root, err := panel.AsUIElement()
	if err != nil {
		return err
	}
	defer root.Release()
	if err := ready.Window.SetContent(root); err != nil {
		return err
	}

	// Closing the window has to end the message loop, and only the UI thread may do
	// that — so the exit is wired to the window's own Closed event.
	closed, err := uixaml.NewTypedEventHandlerOfObjectAndWindowEventArgs(
		func(_ *syswinrt.IInspectable, _ *uixaml.IWindowEventArgs) {
			_ = ready.Application.Exit()
		})
	if err != nil {
		return err
	}
	if _, err := ready.Window.AddClosed(closed); err != nil {
		return err
	}

	return ready.Window.Activate()
}

// setButtonText boxes a string the way WinRT boxes one and assigns it as the Button's
// Content, which is declared on ContentControl three classes above Button.
func setButtonText(button *uixamlcontrols.Button, text string) error {
	content, err := button.AsContentControl()
	if err != nil {
		return err
	}
	defer content.Release()

	propertyValue, err := wrtfoundation.PropertyValueStatics()
	if err != nil {
		return err
	}
	defer propertyValue.Release()
	boxed, err := propertyValue.CreateString(text)
	if err != nil {
		return err
	}
	defer boxed.Release()
	return content.SetContent(boxed)
}

// addChildren appends elements to a Panel's Children collection.
//
// IPanel.Children is typed IInspectable in the projection rather than
// IVector<UIElement>: the generic instantiation could not be named where the property
// is declared, so it degraded. The vector is still there at the ABI, and the
// monomorphized IVectorOfUIElement emitted into this package names it — so one
// QueryInterface recovers the typed collection.
func addChildren(panel *uixamlcontrols.StackPanel, elements ...interface {
	AsUIElement() (*uixaml.IUIElement, error)
}) error {
	// Children is declared on Panel, one class above StackPanel — reached through the
	// generated base-class accessor.
	asPanel, err := panel.AsPanel()
	if err != nil {
		return err
	}
	defer asPanel.Release()

	raw, err := asPanel.Children()
	if err != nil {
		return err
	}
	defer raw.Release()

	children, err := winrt.QueryInterface[uixamlcontrols.IVectorOfUIElement](
		unsafe.Pointer(raw), &uixamlcontrols.IID_IVectorOfUIElement)
	if err != nil {
		return err
	}
	defer children.Release()

	for _, element := range elements {
		child, err := element.AsUIElement()
		if err != nil {
			return err
		}
		if err := children.Append(child); err != nil {
			child.Release()
			return err
		}
		child.Release()
	}
	return nil
}
