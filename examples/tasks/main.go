//go:build windows && amd64

// Command tasks is a second, more demanding WinUI 3 application in Go.
//
// The clicker example is a window with three controls. This one exists to stretch the
// parts of the SDK that a single button never touches, because that is how the
// ergonomic gaps get found:
//
//   - a Grid with row definitions, and elements positioned by attached property
//
//   - a ListView whose items are boxed values
//
//   - a ListView bound to a collection of boxed values
//
//   - work on a background goroutine, marshalled back to the UI thread through the
//     DispatcherQueue
//
//   - an async ContentDialog, awaited without blocking the UI thread
//
//     go run ./examples/tasks
//
// Needs the redistributable bootstrapper (go run ./cmd/generate fetch-bootstrap) and
// the Windows App SDK 2.x runtime.
package main

import (
	"fmt"
	"os"
	"time"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uidispatching "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/dispatching"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func main() {
	if err := app.Run(build, app.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, "tasks:", err)
		os.Exit(1)
	}
}

func build(ready *app.Ready) error {
	grid, err := uixaml.NewGrid()
	if err != nil {
		return err
	}
	addButton, err := uixaml.NewButton()
	if err != nil {
		return err
	}
	list, err := uixaml.NewListView()
	if err != nil {
		return err
	}
	status, err := uixaml.NewTextBlock()
	if err != nil {
		return err
	}

	if err := app.All(
		ready.Window.SetTitle("Tasks — WinUI 3 from Go"),
		app.SetContent(addButton.AsContentControl, "Add"),
		status.SetText("Ready."),
	); err != nil {
		return err
	}

	// Three rows: input, list, status. RowDefinitions is a typed collection now, but
	// each RowDefinition still has to be constructed and appended one at a time.
	// Grid's DEFAULT interface is IGrid, which the class embeds — so its members are
	// called directly, with no query. Only the other interfaces need As<Interface>.
	{
		rows, err := grid.RowDefinitions()
		if err != nil {
			return err
		}
		defer rows.Release()
		for _, height := range []uixaml.GridLength{
			{Value: 0, GridUnitType: uixaml.GridUnitTypeAuto},
			{Value: 1, GridUnitType: uixaml.GridUnitTypeStar},
			{Value: 0, GridUnitType: uixaml.GridUnitTypeAuto},
		} {
			row, err := uixaml.NewRowDefinition()
			if err != nil {
				return err
			}
			if err := row.SetHeight(height); err != nil {
				return err
			}
			if err := rows.Append(&row.IRowDefinition); err != nil {
				return err
			}
		}
		if err := grid.SetRowSpacing(8); err != nil {
			return err
		}
	}

	// Position each child by attached property. In C# this is Grid.SetRow(x, 0); here
	// it is a statics accessor plus a query to IFrameworkElement per element.
	statics, err := uixaml.GridStatics()
	if err != nil {
		return err
	}
	defer statics.Release()
	for row, element := range []interface {
		AsFrameworkElement() (*uixaml.IFrameworkElement, error)
	}{addButton, list, status} {
		if err := app.With(element.AsFrameworkElement, func(fe *uixaml.IFrameworkElement) error {
			return statics.SetRow(fe, int32(row))
		}); err != nil {
			return err
		}
	}

	if err := app.Append(grid.AsPanel, addButton.AsUIElement, list.AsUIElement, status.AsUIElement); err != nil {
		return err
	}

	// Adding an item: box a string and append it to the ListView's Items.
	//
	// This would read a TextBox, and cannot: TextBox is one of the controls whose default
	// style cannot load, and it kills the process when laid out. See
	// TestControlsNeedingThemeResourcesCannotLoad in acceptance/.
	added := 0
	add := func() {
		added++
		text := fmt.Sprintf("Task %d", added)
		if err := app.With(list.AsItemsControl, func(items *uixaml.IItemsControl) error {
			observable, err := items.Items()
			if err != nil {
				return err
			}
			defer observable.Release()
			// Items is an IObservableVector, which carries only its VectorChanged
			// event; adding goes through the IVector it requires.
			collection, err := observable.AsVectorOfObject()
			if err != nil {
				return err
			}
			defer collection.Release()
			boxed, err := app.Box(text)
			if err != nil {
				return err
			}
			defer boxed.Release()
			return collection.Append(boxed)
		}); err != nil {
			_ = status.SetText("Could not add: " + err.Error())
			return
		}
		_ = status.SetText(fmt.Sprintf("%d item(s).", added))
	}

	if err := app.With(addButton.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, err := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { add() })
		return err
	}); err != nil {
		return err
	}

	// Background work marshalled back to the UI thread. Touching a XAML object from
	// another goroutine is an unmarshalled cross-apartment call, so the result has to
	// come back through the DispatcherQueue.
	queue, err := ready.Window.DispatcherQueue()
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(2 * time.Second)
		handler, err := uidispatching.NewDispatcherQueueHandler(func() {
			_ = status.SetText("Background work finished on the UI thread.")
		})
		if err != nil {
			return
		}
		_, _ = queue.TryEnqueue(handler) // handler deliberately NOT closed
	}()

	if err := app.With(grid.AsUIElement, ready.Window.SetContent); err != nil {
		return err
	}
	return ready.Window.Activate()
}
