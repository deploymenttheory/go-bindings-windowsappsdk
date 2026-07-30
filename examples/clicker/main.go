//go:build windows && amd64

// Command clicker is a WinUI 3 application in about a hundred lines of Go.
//
// It puts a real window on screen with a StackPanel, a TextBlock and a Button. The
// Button's Click handler is a Go closure that updates the TextBlock, and so are the
// panel's PointerEntered and KeyDown handlers. Nothing here is a test harness: it runs
// until you close the window.
//
// Click the button, move the mouse over the panel, then type — all three arrive as Go
// function calls on the UI thread.
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

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
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

	panel, err := uixaml.NewStackPanel()
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

	label, err := uixaml.NewTextBlock()
	if err != nil {
		return err
	}
	if err := label.SetText("Click the button, move the mouse, or press a key."); err != nil {
		return err
	}

	button, err := uixaml.NewButton()
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
	handler, err := uixaml.NewRoutedEventHandler(
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

	// Click is declared on Primitives.IButtonBase, two classes above Button. There is a
	// generated accessor for it because Controls and Controls.Primitives share one Go
	// package — before they did, this line was a hand-written QueryInterface.
	base, err := button.AsButtonBase()
	if err != nil {
		return err
	}
	defer base.Release()
	if _, err := base.AddClick(handler); err != nil {
		return err
	}

	// Pointer and keyboard events. These are declared on UIElement and take argument
	// types from Microsoft.UI.Xaml.Input — a different namespace, in the same Go
	// package, because the two reference each other and Go's package is its unit of
	// mutual recursion. Split apart they were unreachable.
	if root, err := panel.AsUIElement(); err == nil {
		defer root.Release()

		enter, err := uixaml.NewPointerEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IPointerRoutedEventArgs) {
				_ = label.SetText("Pointer entered the panel.")
			})
		if err != nil {
			return err
		}
		if _, err := root.AddPointerEntered(enter); err != nil {
			return err
		}

		// The window has to be focusable for keystrokes to arrive, which a Button in
		// the tree provides once it has focus — click it, then type.
		keys, err := uixaml.NewKeyEventHandler(
			func(_ *syswinrt.IInspectable, e *uixaml.IKeyRoutedEventArgs) {
				key, err := e.Key()
				if err != nil {
					return
				}
				_ = label.SetText(fmt.Sprintf("Key pressed: %s", key))
			})
		if err != nil {
			return err
		}
		if _, err := root.AddKeyDown(keys); err != nil {
			return err
		}
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
func setButtonText(button *uixaml.Button, text string) error {
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
// Children comes back as *IVectorOfUIElement — the monomorphized
// IVector<UIElement> — so Append is called directly. UIElementCollection's only
// interface IS that instantiation, and until the emitter resolved a generic default
// interface this property handed back a bare IInspectable that needed a
// hand-written QueryInterface.
func addChildren(panel *uixaml.StackPanel, elements ...interface {
	AsUIElement() (*uixaml.IUIElement, error)
}) error {
	// Children is declared on Panel, one class above StackPanel — reached through the
	// generated base-class accessor.
	asPanel, err := panel.AsPanel()
	if err != nil {
		return err
	}
	defer asPanel.Release()

	children, err := asPanel.Children()
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
