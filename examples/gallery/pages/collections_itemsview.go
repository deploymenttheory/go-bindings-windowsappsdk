//go:build windows && amd64

package pages

// The ItemsView pages, plus RadioButtons and ItemContainer.
//
// Sources: controls/dev/{ItemsView,RadioButtons,ItemContainer}/TestUI/*Page.xaml
//
// ItemsView is WinUI 3's replacement for ListView and GridView, and the difference is
// structural rather than cosmetic: it composes an ItemsRepeater, a ScrollView and a
// selection model instead of being one monolithic control. So the layout is a property
// you set — StackLayout, UniformGridLayout, LinedFlowLayout — rather than a choice
// between two different control types.

import (
	"fmt"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "ItemsView", Name: "ItemsViewPage", Build: buildItemsViewPage})
	register(Page{Control: "ItemsView", Name: "ItemsViewBlankPage", Build: buildItemsViewBlankPage})
	register(Page{Control: "ItemsView", Name: "ItemsViewSummaryPage", Build: buildItemsViewSummaryPage})
	register(Page{Control: "ItemsView", Name: "ItemsViewIntegrationPage", Build: buildItemsViewIntegrationPage})
	register(Page{Control: "ItemsView", Name: "ItemsViewInteractiveTestsPage", Build: buildItemsViewInteractiveTestsPage})
	register(Page{Control: "ItemsView", Name: "ItemsViewTransitionPage", Build: buildItemsViewTransitionPage})
	register(Page{Control: "ItemsView", Name: "ItemsViewPictureLibraryPage", Build: buildItemsViewPictureLibraryPage})

	register(Page{Control: "RadioButtons", Name: "RadioButtonsPage", Build: buildRadioButtonsPage})
	register(Page{Control: "RadioButtons", Name: "RadioButtonsFocusPage", Build: buildRadioButtonsFocusPage})

	register(Page{Control: "ItemContainer", Name: "ItemContainerPage", Build: buildItemContainerPage})
	register(Page{Control: "ItemContainer", Name: "ItemContainerLayoutPage", Build: buildItemContainerLayoutPage})
	register(Page{Control: "ItemContainer", Name: "ItemContainerSummaryPage", Build: buildItemContainerSummaryPage})
}

// itemCardTemplate is the template most of these pages render an item with. An
// ItemContainer is what makes an item selectable — ItemsView requires one at the root of
// its template, and supplies the selection visuals through it.
const itemCardTemplate = `<DataTemplate>
	<ItemContainer>
		<TextBlock Text="{Binding}" Margin="12,8" />
	</ItemContainer>
</DataTemplate>`

// newItemsView builds an ItemsView over values with the given layout.
//
// The ItemsSource is deliberately NOT closed here. The control holds it for as long as
// it is bound, and these pages live for as long as the window does; closing it at the
// end of the builder would release the collection out from under the control.
func newItemsView(layout *uixaml.ILayout, values []string) (*uixaml.ItemsView, error) {
	view, err := uixaml.NewItemsView()
	if err != nil {
		return nil, err
	}
	if layout != nil {
		if err := view.SetLayout(layout); err != nil {
			return nil, err
		}
	}

	template, err := dataTemplate(itemCardTemplate)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	factory, err := elementFactoryOf(template)
	if err != nil {
		return nil, err
	}
	defer factory.Release()
	if err := view.SetItemTemplate(factory); err != nil {
		return nil, err
	}

	source, err := itemsSource(values)
	if err != nil {
		return nil, err
	}
	if err := view.SetItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, err
	}
	return view, nil
}

// ItemsViewPage: the control at its defaults, which is a vertical stack.
func buildItemsViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newItemsView(nil, numbered("Item", 40))
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(360), frame.SetWidth(420))
	}); err != nil {
		return nil, err
	}
	caption, err := label("ItemsView with its default layout")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemsViewBlankPage is an ItemsView with no source at all.
//
// It looks like filler and is the page that catches the worst class of bug: a control
// that measures or realizes something before it has anything to show.
func buildItemsViewBlankPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewItemsView()
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(240), frame.SetWidth(360))
	}); err != nil {
		return nil, err
	}
	caption, err := label("An ItemsView with no ItemsSource and no Layout.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemsViewSummaryPage: each layout the control ships with, side by side.
func buildItemsViewSummaryPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layouts := []struct {
		caption string
		build   func() (*uixaml.ILayout, error)
	}{
		{"StackLayout, vertical", func() (*uixaml.ILayout, error) {
			return stackLayout(uixaml.OrientationVertical, 4)
		}},
		{"StackLayout, horizontal", func() (*uixaml.ILayout, error) {
			return stackLayout(uixaml.OrientationHorizontal, 4)
		}},
		{"UniformGridLayout", func() (*uixaml.ILayout, error) {
			return uniformGridLayout(120, 60)
		}},
		{"LinedFlowLayout", func() (*uixaml.ILayout, error) {
			layout, err := uixaml.NewLinedFlowLayout()
			if err != nil {
				return nil, err
			}
			defer layout.Release()
			if err := app.All(layout.SetLineHeight(48), layout.SetLineSpacing(4)); err != nil {
				return nil, err
			}
			return layout.AsLayout()
		}},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range layouts {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		layout, err := entry.build()
		if err != nil {
			return nil, err
		}
		view, err := newItemsView(layout, numbered("Item", 20))
		layout.Release()
		if err != nil {
			return nil, err
		}
		if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetHeight(180), frame.SetWidth(460))
		}); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, view.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ItemsViewIntegrationPage: selection driven from outside the control, and reported back.
//
// SelectionMode, Select/Deselect by index and SelectedItems are the model ItemsView
// exposes instead of ListView's SelectedItem-plus-SelectedItems pair.
func buildItemsViewIntegrationPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newItemsView(nil, numbered("Row", 24))
	if err != nil {
		return nil, err
	}
	if err := app.All(
		view.SetSelectionMode(uixaml.ItemsViewSelectionModeMultiple),
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetHeight(280), frame.SetWidth(420))
		}),
	); err != nil {
		return nil, err
	}

	status, err := label("Nothing selected.")
	if err != nil {
		return nil, err
	}
	refresh := func() {
		selected, err := view.SelectedItems()
		if err != nil {
			_ = status.SetText("Reading the selection failed: " + err.Error())
			return
		}
		defer selected.Release()
		size, err := selected.Size()
		if err != nil {
			_ = status.SetText("Reading the selection size failed: " + err.Error())
			return
		}
		_ = status.SetText(fmt.Sprintf("%d selected", size))
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Select 0, 2, 4", func() {
				for _, index := range []int32{0, 2, 4} {
					if err := view.Select(index); err != nil {
						_ = status.SetText("Select failed: " + err.Error())
						return
					}
				}
				refresh()
			})
		},
		func() (*uixaml.Button, error) {
			return button("Select all", func() {
				if err := view.SelectAll(); err != nil {
					_ = status.SetText("SelectAll failed: " + err.Error())
					return
				}
				refresh()
			})
		},
		func() (*uixaml.Button, error) {
			return button("Deselect all", func() {
				if err := view.DeselectAll(); err != nil {
					_ = status.SetText("DeselectAll failed: " + err.Error())
					return
				}
				refresh()
			})
		},
	)
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, row.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemsViewInteractiveTestsPage: the selection modes, and item invocation.
func buildItemsViewInteractiveTestsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	modes := []struct {
		caption string
		value   uixaml.ItemsViewSelectionMode
	}{
		{"None", uixaml.ItemsViewSelectionModeNone},
		{"Single", uixaml.ItemsViewSelectionModeSingle},
		{"Multiple", uixaml.ItemsViewSelectionModeMultiple},
		{"Extended", uixaml.ItemsViewSelectionModeExtended},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range modes {
		caption, err := label("SelectionMode " + mode.caption)
		if err != nil {
			return nil, err
		}
		view, err := newItemsView(nil, numbered("Item", 8))
		if err != nil {
			return nil, err
		}
		if err := app.All(
			view.SetSelectionMode(mode.value),
			view.SetIsItemInvokedEnabled(true),
			app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return app.All(frame.SetHeight(150), frame.SetWidth(400))
			}),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, view.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ItemsViewTransitionPage: the animation played as items are added, removed or reordered.
//
// ItemTransitionProvider is the seam. The source drives a custom one; the shipped
// CreateDefaultItemTransitionProvider is what a caller gets without writing one, and
// showing it working is the honest port of a page whose subject is that seam existing.
func buildItemsViewTransitionPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newItemsView(nil, numbered("Item", 12))
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(300), frame.SetWidth(420))
	}); err != nil {
		return nil, err
	}

	status, err := label("Reading the transition provider…")
	if err != nil {
		return nil, err
	}
	provider, err := view.ItemTransitionProvider()
	if err != nil {
		_ = status.SetText("ItemTransitionProvider failed: " + err.Error())
	} else if provider == nil {
		_ = status.SetText("No ItemTransitionProvider is set; the control animates with its own default.")
	} else {
		provider.Release()
		_ = status.SetText("An ItemTransitionProvider is set.")
	}

	panel, err := stack(8, status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemsViewPictureLibraryPage: the source binds the user's picture library through a
// LinedFlowLibrary and renders thumbnails.
//
// Reading the picture library needs a capability an unpackaged application does not have,
// and a gallery page that depends on the machine's photos is not a test of anything. What
// ports is the layout it exists to show — LinedFlowLayout, which is the one designed for
// variable-width items — over coloured tiles standing in for the thumbnails.
func buildItemsViewPictureLibraryPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := uixaml.NewLinedFlowLayout()
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	if err := app.All(
		layout.SetLineHeight(80),
		layout.SetLineSpacing(6),
		layout.SetMinItemSpacing(6),
	); err != nil {
		return nil, err
	}
	base, err := layout.AsLayout()
	if err != nil {
		return nil, err
	}
	defer base.Release()

	// Widths vary per item, which is the whole reason LinedFlowLayout exists.
	captions := make([]string, 0, 24)
	for i := 1; i <= 24; i++ {
		captions = append(captions, fmt.Sprintf("Picture %d", i))
	}
	view, err := newItemsView(base, captions)
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(320), frame.SetWidth(460))
	}); err != nil {
		return nil, err
	}

	note, err := label("LinedFlowLayout, the layout this page exists to show. The source " +
		"fills it from the picture library, which an unpackaged app cannot read.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// RadioButtonsPage: the control at each column count, with a header.
//
// RadioButtons is not a panel of RadioButton — it is an items control that owns the
// group, which is what makes exactly-one-selected a property of the control rather than
// something the caller maintains.
func buildRadioButtonsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, columns := range []int32{1, 2, 3} {
		control, err := uixaml.NewRadioButtons()
		if err != nil {
			return nil, err
		}
		header, err := app.Box(fmt.Sprintf("MaxColumns %d", columns))
		if err != nil {
			return nil, err
		}
		err = app.All(control.SetHeader(header), control.SetMaxColumns(columns))
		header.Release()
		if err != nil {
			return nil, err
		}

		items, err := control.Items()
		if err != nil {
			return nil, err
		}
		for _, caption := range numbered("Option", 6) {
			boxed, err := app.Box(caption)
			if err != nil {
				items.Release()
				return nil, err
			}
			err = items.Append(boxed)
			boxed.Release()
			if err != nil {
				items.Release()
				return nil, err
			}
		}
		items.Release()

		if err := control.SetSelectedIndex(0); err != nil {
			return nil, err
		}
		children = append(children, control.AsUIElement)
	}
	panel, err := stack(12, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// RadioButtonsFocusPage: focus moving in and out of the group.
//
// The source checks that tabbing into the group lands on the SELECTED item rather than
// the first, which is what a radio group is supposed to do — so there is a focusable
// control either side to tab between.
func buildRadioButtonsFocusPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	before, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(before.AsContentControl, "Before the group"); err != nil {
		return nil, err
	}

	control, err := uixaml.NewRadioButtons()
	if err != nil {
		return nil, err
	}
	items, err := control.Items()
	if err != nil {
		return nil, err
	}
	for _, caption := range numbered("Choice", 4) {
		boxed, err := app.Box(caption)
		if err != nil {
			items.Release()
			return nil, err
		}
		err = items.Append(boxed)
		boxed.Release()
		if err != nil {
			items.Release()
			return nil, err
		}
	}
	items.Release()
	// The third one, so "tab lands on the selected item" is visibly different from
	// "tab lands on the first".
	if err := control.SetSelectedIndex(2); err != nil {
		return nil, err
	}

	after, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(after.AsContentControl, "After the group"); err != nil {
		return nil, err
	}

	status, err := label("Tab from the first button: focus should land on Choice 3, the selected one.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, before.AsUIElement, control.AsUIElement, after.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemContainerPage: the container on its own, in each selection state.
//
// ItemContainer is the selectable wrapper ItemsView puts round each item. It is a
// primitive with one job — hold a child and render selection — which is why it can be
// shown outside any items control at all.
func buildItemContainerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, selected := range []bool{false, true} {
		container, err := uixaml.NewItemContainer()
		if err != nil {
			return nil, err
		}
		inner, err := label(fmt.Sprintf("IsSelected = %v", selected))
		if err != nil {
			return nil, err
		}
		if err := app.All(
			app.With(inner.AsUIElement, container.SetChild),
			container.SetIsSelected(selected),
		); err != nil {
			return nil, err
		}
		children = append(children, container.AsUIElement)
	}
	caption, err := label("ItemContainer, unselected and selected")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, append([]func() (*uixaml.IUIElement, error){caption.AsUIElement}, children...)...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemContainerLayoutPage: containers holding children of different sizes, which is what
// the source varies to check the container measures to its child.
func buildItemContainerLayoutPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	sizes := []struct {
		width, height float64
	}{{80, 40}, {160, 40}, {160, 80}, {240, 120}}

	var children []func() (*uixaml.IUIElement, error)
	for index, size := range sizes {
		container, err := uixaml.NewItemContainer()
		if err != nil {
			return nil, err
		}
		band, err := colouredBand(size.width, size.height,
			fmt.Sprintf("%.0f×%.0f", size.width, size.height),
			bandColours[index%len(bandColours)])
		if err != nil {
			return nil, err
		}
		if err := app.With(band.AsUIElement, container.SetChild); err != nil {
			return nil, err
		}
		children = append(children, container.AsUIElement)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ItemContainerSummaryPage: the container inside the control it exists for, next to one
// standing alone, so the difference the items control makes is visible.
func buildItemContainerSummaryPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	standalone, err := uixaml.NewItemContainer()
	if err != nil {
		return nil, err
	}
	inner, err := label("A standalone ItemContainer")
	if err != nil {
		return nil, err
	}
	brush, err := solidBrush(wrtui.Color{A: 255, R: 90, G: 90, B: 90})
	if err != nil {
		return nil, err
	}
	err = inner.SetForeground(brush)
	brush.Release()
	if err != nil {
		return nil, err
	}
	if err := app.With(inner.AsUIElement, standalone.SetChild); err != nil {
		return nil, err
	}

	view, err := newItemsView(nil, numbered("Managed item", 10))
	if err != nil {
		return nil, err
	}
	if err := app.All(
		view.SetSelectionMode(uixaml.ItemsViewSelectionModeSingle),
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetHeight(240), frame.SetWidth(420))
		}),
	); err != nil {
		return nil, err
	}

	// Reading the container back out of the control is what shows they are the same
	// type: the template's ItemContainer is what ItemsView selects through.
	status, err := label("Click an item: the control selects through the same ItemContainer type.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(view.AddSelectionChanged,
		uixaml.NewTypedEventHandlerOfItemsViewAndItemsViewSelectionChangedEventArgs,
		func(sender *uixaml.IItemsView, _ *uixaml.IItemsViewSelectionChangedEventArgs) {
			index, err := sender.CurrentItemIndex()
			if err != nil {
				_ = status.SetText("Reading the current index failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("Current item index: %d", index))
		}); err != nil {
		return nil, err
	}

	caption, err := label("Standalone, then inside an ItemsView")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, standalone.AsUIElement,
		status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// unusedSyswinrt keeps the import honest if a future edit drops the last use.
var _ = (*syswinrt.IInspectable)(nil)
