//go:build windows && amd64

package pages

// The remaining items-control variants, and the two window pages.
//
// Sources: controls/dev/CommonStyles/TestUI/{FlatItemsControl,GroupedItemsControl,
// GroupedListViewBase,ListViewBase,ListViewAnchoring,ListViewElementNameBinding,
// NestedItemsControls,NestedListViews,NestedGridViews,CommandBarSummary,NewWindowRoot,
// Window}Page.xaml

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "FlatItemsControlPage", Build: buildFlatItemsControlPage})
	register(Page{Control: "CommonStyles", Name: "GroupedItemsControlPage", Build: buildGroupedItemsControlPage})
	register(Page{Control: "CommonStyles", Name: "GroupedListViewBasePage", Build: buildGroupedListViewBasePage})
	register(Page{Control: "CommonStyles", Name: "ListViewBasePage", Build: buildListViewBasePage})
	register(Page{Control: "CommonStyles", Name: "ListViewAnchoringPage", Build: buildListViewAnchoringPage})
	register(Page{Control: "CommonStyles", Name: "ListViewElementNameBindingPage", Build: buildListViewElementNameBindingPage})
	register(Page{Control: "CommonStyles", Name: "NestedItemsControlsPage", Build: buildNestedItemsControlsPage})
	register(Page{Control: "CommonStyles", Name: "NestedListViewsPage", Build: buildNestedListViewsPage})
	register(Page{Control: "CommonStyles", Name: "NestedGridViewsPage", Build: buildNestedGridViewsPage})
	register(Page{Control: "CommonStyles", Name: "CommandBarSummaryPage", Build: buildCommandBarSummaryPage})
	register(Page{Control: "CommonStyles", Name: "NewWindowRootPage", Build: buildNewWindowRootPage})
	register(Page{Control: "CommonStyles", Name: "WindowPage", Build: buildWindowPage})
}

// numbered builds n captions, the filler these pages use to give a list a length.
func numbered(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}
	return out
}

// FlatItemsControlPage: a long flat list, which is what the source uses to check the
// panel virtualizes rather than laying out everything at once.
func buildFlatItemsControlPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	control, err := uixaml.NewItemsControl()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(control), numbered("Item", 60)); err != nil {
		return nil, err
	}
	caption, err := label("ItemsControl with 60 items")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// groupedItems builds an ItemsControl whose items are themselves headed lists.
//
// The source groups through a CollectionViewSource with IsSourceGrouped, which needs a
// bindable grouped collection — a shape that has no Go form, because the binding engine
// discovers the groups reflectively. The tree it produces is a header followed by its
// children, and that is what is built here directly.
func groupedItems(groups int, perGroup int) ([]func() (*uixaml.IUIElement, error), error) {
	var children []func() (*uixaml.IUIElement, error)
	for group := 1; group <= groups; group++ {
		header, err := label(fmt.Sprintf("Group %d", group))
		if err != nil {
			return nil, err
		}
		if err := app.With(header.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetMargin(uixaml.Thickness{Top: 8})
		}); err != nil {
			return nil, err
		}
		control, err := uixaml.NewItemsControl()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(control), numbered(fmt.Sprintf("Group %d item", group), perGroup)); err != nil {
			return nil, err
		}
		children = append(children, header.AsUIElement, control.AsUIElement)
	}
	return children, nil
}

func buildGroupedItemsControlPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	children, err := groupedItems(3, 4)
	if err != nil {
		return nil, err
	}
	panel, err := stack(4, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func buildGroupedListViewBasePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for group := 1; group <= 3; group++ {
		header, err := label(fmt.Sprintf("Group %d", group))
		if err != nil {
			return nil, err
		}
		view, err := uixaml.NewListView()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(view), numbered(fmt.Sprintf("G%d item", group), 4)); err != nil {
			return nil, err
		}
		if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(140)
		}); err != nil {
			return nil, err
		}
		children = append(children, header.AsUIElement, view.AsUIElement)
	}
	panel, err := stack(4, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ListViewBasePage: the selection modes ListViewBase defines, which is the class the
// page is named for.
func buildListViewBasePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	modes := []struct {
		caption string
		value   uixaml.ListViewSelectionMode
	}{
		{"None", uixaml.ListViewSelectionModeNone},
		{"Single", uixaml.ListViewSelectionModeSingle},
		{"Multiple", uixaml.ListViewSelectionModeMultiple},
		{"Extended", uixaml.ListViewSelectionModeExtended},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range modes {
		caption, err := label("SelectionMode " + mode.caption)
		if err != nil {
			return nil, err
		}
		view, err := uixaml.NewListView()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(view), numbered("Item", 4)); err != nil {
			return nil, err
		}
		if err := app.All(
			app.With(view.AsListViewBase, func(base *uixaml.IListViewBase) error {
				return base.SetSelectionMode(mode.value)
			}),
			app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return frame.SetHeight(140)
			}),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, view.AsUIElement)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ListViewAnchoringPage: a long list the source scrolls to check the anchor holds
// position. ScrollIntoView is on ListViewBase.
func buildListViewAnchoringPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewListView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(view), numbered("Anchored item", 80)); err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(300)
	}); err != nil {
		return nil, err
	}

	scrollTo, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(scrollTo.AsContentControl, "Scroll to item 40"); err != nil {
		return nil, err
	}
	if err := app.With(scrollTo.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				_ = app.With(view.AsListViewBase, func(listBase *uixaml.IListViewBase) error {
					target, err := app.Box("Anchored item 40")
					if err != nil {
						return err
					}
					defer target.Release()
					return listBase.ScrollIntoView(target)
				})
			})
		return addErr
	}); err != nil {
		return nil, err
	}

	panel, err := stack(8, scrollTo.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ListViewElementNameBindingPage: the source binds an item template to an element
// elsewhere on the page by x:Name. ElementName binding resolves through the XAML
// namescope, which exists only for markup the parser built — so the template is loaded
// as markup, which is the honest port, and the list beside it drives the same value.
func buildListViewElementNameBindingPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewListView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(view), numbered("Bound item", 6)); err != nil {
		return nil, err
	}
	template, err := app.LoadMarkup[uixaml.IDataTemplate](
		app.Markup(`<DataTemplate><TextBlock Text="{Binding}" Margin="4,2"/></DataTemplate>`),
		&uixaml.IID_IDataTemplate)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	if err := app.With(view.AsItemsControl, func(items *uixaml.IItemsControl) error {
		return items.SetItemTemplate(template)
	}); err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(220)
	}); err != nil {
		return nil, err
	}
	caption, err := label("ListView with a templated item")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// nested builds an items control whose items are themselves items controls, which is
// what the three Nested* pages check the panels do about measurement.
func nested(outer func() (*uixaml.IUIElement, error), makeInner func() (*uixaml.IUIElement, error), count int) error {
	return app.With(outer, func(element *uixaml.IUIElement) error {
		items, err := winrt.QueryInterface[uixaml.IItemsControl](
			unsafe.Pointer(element), &uixaml.IID_IItemsControl)
		if err != nil {
			return err
		}
		defer items.Release()
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

		for i := 0; i < count; i++ {
			inner, err := makeInner()
			if err != nil {
				return err
			}
			err = collection.Append(&inner.IInspectable)
			inner.Release()
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func buildNestedItemsControlsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	outer, err := uixaml.NewItemsControl()
	if err != nil {
		return nil, err
	}
	err = nested(outer.AsUIElement, func() (*uixaml.IUIElement, error) {
		inner, err := uixaml.NewItemsControl()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(inner), numbered("Inner", 3)); err != nil {
			return nil, err
		}
		return inner.AsUIElement()
	}, 3)
	if err != nil {
		return nil, err
	}
	return scrolled(outer.AsUIElement)
}

func buildNestedListViewsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	outer, err := uixaml.NewItemsControl()
	if err != nil {
		return nil, err
	}
	err = nested(outer.AsUIElement, func() (*uixaml.IUIElement, error) {
		inner, err := uixaml.NewListView()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(inner), numbered("Row", 3)); err != nil {
			return nil, err
		}
		if err := app.With(inner.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(120)
		}); err != nil {
			return nil, err
		}
		return inner.AsUIElement()
	}, 3)
	if err != nil {
		return nil, err
	}
	return scrolled(outer.AsUIElement)
}

func buildNestedGridViewsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	outer, err := uixaml.NewItemsControl()
	if err != nil {
		return nil, err
	}
	err = nested(outer.AsUIElement, func() (*uixaml.IUIElement, error) {
		inner, err := uixaml.NewGridView()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(inner), numbered("Cell", 4)); err != nil {
			return nil, err
		}
		if err := app.With(inner.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(140)
		}); err != nil {
			return nil, err
		}
		return inner.AsUIElement()
	}, 2)
	if err != nil {
		return nil, err
	}
	return scrolled(outer.AsUIElement)
}

// CommandBarSummaryPage: several bars at once, which is how the source compares label
// positions side by side.
func buildCommandBarSummaryPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	positions := []struct {
		caption string
		value   uixaml.CommandBarDefaultLabelPosition
	}{
		{"Bottom", uixaml.CommandBarDefaultLabelPositionBottom},
		{"Right", uixaml.CommandBarDefaultLabelPositionRight},
		{"Collapsed", uixaml.CommandBarDefaultLabelPositionCollapsed},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, position := range positions {
		caption, err := label("DefaultLabelPosition " + position.caption)
		if err != nil {
			return nil, err
		}
		bar, err := commandBar(position.value)
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, bar.AsUIElement)
	}
	panel, err := stack(12, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// NewWindowRootPage and WindowPage both open a second Window.
//
// A page hands back an element for the gallery to host, so the window cannot BE the
// page — the port is the button that opens one, which is what the source's page does
// too. The new window owns itself once activated.
func buildNewWindowRootPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return windowOpener("Open a second window", func() error {
		window, err := uixaml.NewWindow()
		if err != nil {
			return err
		}
		content, err := label("A second window, with its own XAML tree.")
		if err != nil {
			return err
		}
		if err := app.All(
			window.SetTitle("Second window"),
			app.With(content.AsUIElement, window.SetContent),
		); err != nil {
			return err
		}
		return window.Activate()
	})
}

func buildWindowPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return windowOpener("Open a window with a Grid root", func() error {
		window, err := uixaml.NewWindow()
		if err != nil {
			return err
		}
		grid, err := gridOf([]uixaml.GridLength{auto(), star(1)}, nil)
		if err != nil {
			return err
		}
		heading, err := label("Row 0")
		if err != nil {
			return err
		}
		body, err := label("Row 1, taking the remaining height")
		if err != nil {
			return err
		}
		if err := app.Append(grid.AsPanel, heading.AsUIElement, body.AsUIElement); err != nil {
			return err
		}
		place, err := newPlacement()
		if err != nil {
			return err
		}
		defer place.Close()
		if err := place.at(body.AsUIElement, 1, 0); err != nil {
			return err
		}
		if err := app.All(
			window.SetTitle("Window with a Grid root"),
			app.With(grid.AsUIElement, window.SetContent),
		); err != nil {
			return err
		}
		return window.Activate()
	})
}

// windowOpener builds the button both window pages are.
func windowOpener(caption string, open func() error) (*uixaml.IUIElement, error) {
	status, err := label("No window opened yet.")
	if err != nil {
		return nil, err
	}
	button, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(button.AsContentControl, caption); err != nil {
		return nil, err
	}
	if err := app.With(button.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				if err := open(); err != nil {
					_ = status.SetText("Could not open: " + err.Error())
					return
				}
				_ = status.SetText("Opened.")
			})
		return addErr
	}); err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, button.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}
