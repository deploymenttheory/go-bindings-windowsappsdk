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
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uiinput "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/input"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// bindingFailurePrefix marks a binding failure on stderr so a sweep can pick the
// lines out without parsing anything else the gallery says.
const bindingFailurePrefix = "BINDING-FAILED: "

func main() {
	// A binding that cannot resolve its path renders nothing and reports nothing:
	// the page still builds, still lays out, and still passes the conformance
	// suite. Printing every failure is what makes the difference between a page
	// that works and one that merely appears in the tree observable from outside.
	// Announce the diagnostic on the same channel the failures use. Without it a
	// silent log is ambiguous — it could mean no binding failed, or that nothing
	// this process writes to stderr is being captured at all — and those two
	// call for opposite conclusions.
	fmt.Fprintln(os.Stderr, "gallery: binding diagnostics active")
	options := app.Options{
		TraceBindings: true,
		OnBindingFailed: func(message string) {
			fmt.Fprintln(os.Stderr, bindingFailurePrefix+message)
		},
	}
	if err := app.Run(build, options); err != nil {
		fmt.Fprintln(os.Stderr, "gallery:", err)
		os.Exit(1)
	}
}

func build(ready *app.Ready) error {
	registered := pages.Buildable()
	if len(registered) == 0 {
		return fmt.Errorf("gallery: no pages are registered")
	}

	// Three columns: the page list, a draggable splitter, then whatever it selects.
	//
	// The splitter is a Thumb rather than a GridSplitter, because there is no
	// GridSplitter to use: it belongs to the Community Toolkit, not the Windows App SDK,
	// so it is absent from the metadata this repository projects. Thumb is the primitive
	// GridSplitter itself is built on — it reports DragStarted, DragDelta and
	// DragCompleted and draws nothing of its own — so the whole behaviour is one handler
	// that moves the first column's width.
	root, err := uixaml.NewGrid()
	if err != nil {
		return err
	}
	if err := addColumns(root, []uixaml.GridLength{
		{Value: defaultListWidth, GridUnitType: uixaml.GridUnitTypePixel},
		{Value: splitterWidth, GridUnitType: uixaml.GridUnitTypePixel},
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
	// Without a template a ListViewItem trims a long string to an ellipsis, and the
	// keys here are long — "ScrollPresenter/ScrollPresenterWithSimpleScrollControllersPage"
	// is 62 characters. Wrapping means narrowing the column hides nothing.
	itemTemplate, err := app.LoadMarkup[uixaml.IDataTemplate](
		app.Markup(`<DataTemplate><TextBlock Text="{Binding}" TextWrapping="Wrap"/></DataTemplate>`),
		&uixaml.IID_IDataTemplate)
	if err != nil {
		return err
	}
	defer itemTemplate.Release()
	if err := app.With(list.AsItemsControl, func(items *uixaml.IItemsControl) error {
		return items.SetItemTemplate(itemTemplate)
	}); err != nil {
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
		// Tear the outgoing page down FIRST. Dropping it from the ContentControl is not
		// enough on its own: anything it opened in the popup root — a flyout, a tooltip,
		// AnnotatedScrollBar's detail label — is parented to the XamlRoot rather than to
		// the page, so it outlives the tree it came from. Left alone they accumulate,
		// one per visit, floating over whatever is shown next.
		_ = app.With(host.AsContentControl, func(content *uixaml.IContentControl) error {
			return content.SetContent(nil)
		})
		app.CloseOpenPopups(ready)

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

	splitter, err := newSplitter(root)
	if err != nil {
		return err
	}

	if err := app.Append(root.AsPanel,
		list.AsUIElement, splitter.AsUIElement, host.AsUIElement); err != nil {
		return err
	}
	statics, err := uixaml.GridStatics()
	if err != nil {
		return err
	}
	defer statics.Release()
	if err := app.With(splitter.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return statics.SetColumn(frame, 1)
	}); err != nil {
		return err
	}
	if err := app.With(host.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return statics.SetColumn(frame, 2)
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

// Splitter geometry. The list starts wide enough for the longest key at the default
// font, and can be dragged between the two bounds below — narrow enough to be out of the
// way, never so narrow or so wide that one side becomes unusable.
const (
	defaultListWidth = 340.0
	splitterWidth    = 8.0
	minListWidth     = 140.0
	minContentWidth  = 260.0
)

// newSplitter builds the draggable divider between the two columns.
func newSplitter(root *uixaml.Grid) (*uixaml.Thumb, error) {
	thumb, err := uixaml.NewThumb()
	if err != nil {
		return nil, err
	}

	if err := app.With(thumb.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(
			frame.SetWidth(splitterWidth),
			frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentStretch),
			frame.SetVerticalAlignment(uixaml.VerticalAlignmentStretch),
		)
	}); err != nil {
		return nil, err
	}

	// A west-east cursor is what tells a user the divider can be moved at all. It is set
	// on the element rather than in a style because ProtectedCursor is a property, not a
	// template concern.
	if err := app.With(thumb.AsUIElementProtected, func(protected *uixaml.IUIElementProtected) error {
		statics, err := uiinput.InputSystemCursorStatics()
		if err != nil {
			return err
		}
		defer statics.Release()
		cursor, err := statics.Create(uiinput.InputSystemCursorShapeSizeWestEast)
		if err != nil {
			return err
		}
		defer cursor.Release()
		// Create returns the derived IInputSystemCursor; the property takes the base
		// IInputCursor. They are the same object, so this is a plain query — an
		// InputSystemCursor IS an InputCursor, but the projection surfaces each
		// interface separately and only the class carries an As accessor.
		base, err := winrt.QueryInterface[uiinput.IInputCursor](
			unsafe.Pointer(cursor), &uiinput.IID_IInputCursor)
		if err != nil {
			return err
		}
		defer base.Release()
		return protected.SetProtectedCursor(base)
	}); err != nil {
		return nil, err
	}

	// DragDelta reports the movement since the LAST event, not since the drag began, so
	// the width accumulates rather than being recomputed from an origin.
	if err := app.With(thumb.AsThumb, func(control *uixaml.IThumb) error {
		_, addErr := control.AddDragDelta(mustDragHandler(root))
		return addErr
	}); err != nil {
		return nil, err
	}
	return thumb, nil
}

// mustDragHandler builds the DragDelta handler that resizes the first column.
func mustDragHandler(root *uixaml.Grid) *uixaml.DragDeltaEventHandler {
	handler, err := uixaml.NewDragDeltaEventHandler(
		func(_ *syswinrt.IInspectable, args *uixaml.IDragDeltaEventArgs) {
			change, err := args.HorizontalChange()
			if err != nil {
				return
			}
			_ = resizeFirstColumn(root, change)
		})
	if err != nil {
		return nil
	}
	return handler
}

// resizeFirstColumn moves the divider by delta, within the bounds above.
func resizeFirstColumn(root *uixaml.Grid, delta float64) error {
	columns, err := root.ColumnDefinitions()
	if err != nil {
		return err
	}
	defer columns.Release()
	first, err := columns.GetAt(0)
	if err != nil {
		return err
	}
	defer first.Release()

	width, err := first.Width()
	if err != nil {
		return err
	}
	wanted := width.Value + delta
	if wanted < minListWidth {
		wanted = minListWidth
	}
	// The upper bound depends on the window, so it is read rather than assumed: a fixed
	// maximum would be wrong the moment the window is resized. app.With cannot carry a
	// value out, so this is the plain query.
	if frame, err := root.AsFrameworkElement(); err == nil {
		available, widthErr := frame.ActualWidth()
		frame.Release()
		if widthErr == nil && available > 0 {
			if limit := available - splitterWidth - minContentWidth; wanted > limit {
				wanted = limit
			}
		}
	}
	return first.SetWidth(uixaml.GridLength{
		Value: wanted, GridUnitType: uixaml.GridUnitTypePixel})
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
