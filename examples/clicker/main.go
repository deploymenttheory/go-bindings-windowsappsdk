//go:build windows && amd64

// Command clicker is a WinUI 3 application in about sixty lines of Go.
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

	if err := app.All(
		ready.Window.SetTitle("Clicker — WinUI 3 from Go"),
		panel.SetSpacing(16),
		panel.SetPadding(uixaml.Thickness{Left: 32, Top: 32, Right: 32, Bottom: 32}),
		label.SetText("Click the button, move the mouse, or press a key."),
		// Content is declared on ContentControl, three classes above Button, and takes
		// a boxed IInspectable rather than a string.
		app.SetContent(button.AsContentControl, "Click me"),
	); err != nil {
		return err
	}

	// Centre the stack rather than letting it stretch to the corners. Alignment is on
	// FrameworkElement; With queries it, runs the block, and releases it.
	if err := app.With(panel.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(
			frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentCenter),
			frame.SetVerticalAlignment(uixaml.VerticalAlignmentCenter),
		)
	}); err != nil {
		return err
	}

	// The handlers are ordinary Go closures. They run on the UI thread — the framework
	// invokes them there and go-bindings-winrt's inline-thread mode keeps them there —
	// so they may touch XAML objects directly.
	//
	// Click is declared on Primitives.IButtonBase, two classes above Button; the
	// pointer and keyboard events are on UIElement and take their argument types from
	// Microsoft.UI.Xaml.Input. All four namespaces share one Go package, because they
	// reference each other and Go's package is its unit of mutual recursion.
	clicks := 0
	if err := app.With(button.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, err := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				clicks++
				word := "times"
				if clicks == 1 {
					word = "time"
				}
				_ = label.SetText(fmt.Sprintf("Clicked %d %s.", clicks, word))
			})
		return err
	}); err != nil {
		return err
	}

	if err := app.With(panel.AsUIElement, func(root *uixaml.IUIElement) error {
		if _, err := app.On(root.AddPointerEntered, uixaml.NewPointerEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IPointerRoutedEventArgs) {
				_ = label.SetText("Pointer entered the panel.")
			}); err != nil {
			return err
		}
		// The window has to be focusable for keystrokes to arrive, which the Button
		// provides once it has focus — click it, then type.
		_, err := app.On(root.AddKeyDown, uixaml.NewKeyEventHandler,
			func(_ *syswinrt.IInspectable, e *uixaml.IKeyRoutedEventArgs) {
				if key, err := e.Key(); err == nil {
					_ = label.SetText(fmt.Sprintf("Key pressed: %s", key))
				}
			})
		return err
	}); err != nil {
		return err
	}

	// Children is declared on Panel, one class above StackPanel, and holds UIElements.
	if err := app.Append(panel.AsPanel, label.AsUIElement, button.AsUIElement); err != nil {
		return err
	}

	// Closing the window has to end the message loop, and only the UI thread may do
	// that — so the exit is wired to the window's own Closed event.
	if _, err := app.On(ready.Window.AddClosed, uixaml.NewTypedEventHandlerOfObjectAndWindowEventArgs,
		func(_ *syswinrt.IInspectable, _ *uixaml.IWindowEventArgs) {
			_ = ready.Application.Exit()
		}); err != nil {
		return err
	}

	if err := app.With(panel.AsUIElement, ready.Window.SetContent); err != nil {
		return err
	}
	return ready.Window.Activate()
}
