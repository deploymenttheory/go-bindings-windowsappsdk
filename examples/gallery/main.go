//go:build windows && amd64

// Command gallery is the WinUI control gallery, ported to Go.
//
// Each page is a port of one file from microsoft/microsoft-ui-xaml's
// controls/dev/<Control>/TestUI — the pages WinUI's own test application drives. Picking
// a name on the left builds that page on the right.
//
// It is two things at once, and the second matters more. It is a reference: the Go
// beside each page name shows how that control is actually driven. And it is a
// conformance suite: acceptance/gallery_test.go builds every registered page and asserts
// it lays out, so the reference cannot drift from what works.
//
//	go build -o build/gallery.exe ./examples/gallery
//	go run ./cmd/generate app-resources --out build --name gallery
//	./build/gallery.exe
//
// The resources.pri step is not optional. WinUI keeps its control styles in the
// framework's resource index and reaches them through ms-appx:///, which an unpackaged
// application resolves through its OWN index — and `go build` produces none. Without it
// the templated controls do not render. `go run ./examples/gallery` has the same problem
// and no place to put the file, which is why the build is spelled out above.
package main

import (
	"fmt"
	"os"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
)

func main() {
	if err := app.Run(build, app.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, "gallery:", err)
		os.Exit(1)
	}
}

func build(ready *app.Ready) error {
	registered := pages.Buildable()
	if len(registered) == 0 {
		return fmt.Errorf("gallery: no pages are registered")
	}

	// Two columns: the page list, then whatever it selects.
	root, err := uixaml.NewGrid()
	if err != nil {
		return err
	}
	if err := addColumns(root, []uixaml.GridLength{
		{Value: 260, GridUnitType: uixaml.GridUnitTypePixel},
		{Value: 1, GridUnitType: uixaml.GridUnitTypeStar},
	}); err != nil {
		return err
	}

	host, err := uixaml.NewContentControl()
	if err != nil {
		return err
	}
	list, err := uixaml.NewListView()
	if err != nil {
		return err
	}
	if err := app.With(list.AsItemsControl, func(items *uixaml.IItemsControl) error {
		observable, err := items.Items()
		if err != nil {
			return err
		}
		defer observable.Release()
		collection, err := observable.AsVectorOfObject()
		if err != nil {
			return err
		}
		defer collection.Release()
		for _, page := range registered {
			boxed, err := app.Box(page.Key())
			if err != nil {
				return err
			}
			err = collection.Append(boxed)
			boxed.Release()
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// Selecting a name builds that page. Build failures are shown rather than swallowed:
	// this is a conformance tool, and a page that cannot build is the finding.
	show := func(index int) {
		if index < 0 || index >= len(registered) {
			return
		}
		page := registered[index]
		element, err := page.Build(ready)
		if err != nil {
			// Shown, not swallowed: this is a conformance tool, and a page that cannot
			// build is the finding.
			failure, blockErr := uixaml.NewTextBlock()
			if blockErr != nil {
				return
			}
			defer failure.Release()
			_ = failure.SetText(page.Key() + " failed to build:\n" + err.Error())
			_ = failure.SetTextWrapping(uixaml.TextWrappingWrap)
			_ = app.With(failure.AsUIElement, func(failureElement *uixaml.IUIElement) error {
				return app.With(host.AsContentControl, func(content *uixaml.IContentControl) error {
					return content.SetContent(&failureElement.IInspectable)
				})
			})
			return
		}
		defer element.Release()
		_ = app.With(host.AsContentControl, func(content *uixaml.IContentControl) error {
			return content.SetContent(&element.IInspectable)
		})
	}

	if err := app.With(list.AsSelector, func(selector *uixaml.ISelector) error {
		_, addErr := app.On(selector.AddSelectionChanged, uixaml.NewSelectionChangedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.ISelectionChangedEventArgs) {
				index, err := selector.SelectedIndex()
				if err == nil {
					show(int(index))
				}
			})
		return addErr
	}); err != nil {
		return err
	}

	if err := app.Append(root.AsPanel, list.AsUIElement, host.AsUIElement); err != nil {
		return err
	}
	statics, err := uixaml.GridStatics()
	if err != nil {
		return err
	}
	defer statics.Release()
	if err := app.With(host.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return statics.SetColumn(frame, 1)
	}); err != nil {
		return err
	}

	if err := app.All(
		ready.Window.SetTitle("WinUI gallery — Go"),
		app.With(root.AsUIElement, ready.Window.SetContent),
	); err != nil {
		return err
	}
	// Start on the first page so the window is never blank.
	if err := app.With(list.AsSelector, func(selector *uixaml.ISelector) error {
		return selector.SetSelectedIndex(0)
	}); err != nil {
		return err
	}
	return ready.Window.Activate()
}

// addColumns gives a Grid its ColumnDefinitions.
func addColumns(grid *uixaml.Grid, widths []uixaml.GridLength) error {
	columns, err := grid.ColumnDefinitions()
	if err != nil {
		return err
	}
	defer columns.Release()
	for _, width := range widths {
		column, err := uixaml.NewColumnDefinition()
		if err != nil {
			return err
		}
		if err := column.SetWidth(width); err != nil {
			return err
		}
		if err := columns.Append(&column.IColumnDefinition); err != nil {
			return err
		}
	}
	return nil
}
