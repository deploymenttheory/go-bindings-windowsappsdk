//go:build windows && amd64

package pages

// TabView, BreadcrumbBar, PipsPager and SelectorBar. All four are fully projected and
// used here; only PagerControl, at the bottom of this file, is absent from the SDK.
//
// Sources: controls/dev/{TabView,Breadcrumb,PipsPager,SelectorBar,PagerControl}/TestUI
//
// These are the navigation controls that do not host content themselves. NavigationView,
// which does, is in navigation_view.go.
//
// Several sources are named *AxeTestPage. Axe is the accessibility scanner Microsoft runs
// against them, so the page is a plain arrangement of the control in its common states
// with nothing else on it — which is exactly what the scanner needs and what ports here.

import (
	"fmt"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "TabView", Name: "TabViewPage", Build: buildTabViewPage})
	register(Page{Control: "TabView", Name: "TabViewAxeTestPage", Build: buildTabViewAxeTestPage})
	register(Page{Control: "TabView", Name: "TabViewSizingPage", Build: buildTabViewSizingPage})
	register(Page{Control: "TabView", Name: "TabViewTabClosingBehaviorPage", Build: buildTabViewTabClosingBehaviorPage})
	register(Page{Control: "TabView", Name: "TabViewTabItemsSourcePage", Build: buildTabViewTabItemsSourcePage})
	register(Page{Control: "TabView", Name: "MultipleTabViewPage", Build: buildMultipleTabViewPage})

	register(Page{Control: "Breadcrumb", Name: "BreadcrumbBarPage", Build: buildBreadcrumbBarPage})
	register(Page{Control: "Breadcrumb", Name: "BreadcrumbBarAxeTestPage", Build: buildBreadcrumbBarAxeTestPage})

	register(Page{Control: "PipsPager", Name: "PipsPagerPage", Build: buildPipsPagerPage})
	register(Page{Control: "PipsPager", Name: "PipsPagerAxeTestPage", Build: buildPipsPagerAxeTestPage})

	register(Page{Control: "SelectorBar", Name: "SelectorBarPage", Build: buildSelectorBarPage})
	register(Page{Control: "SelectorBar", Name: "SelectorBarSamplePage", Build: buildSelectorBarSamplePage})
	register(Page{Control: "SelectorBar", Name: "SelectorBarSummaryPage", Build: buildSelectorBarSummaryPage})

	// PagerControl is ABSENT from the Windows App SDK winmds — not skipped by this
	// projection, not present to skip. It was a WinUI experiment that never shipped in
	// the Windows App SDK, the same category as SearchBox, InkToolbar and InkCanvas.
	//
	// Verified against the WINMDS, not against the generated output, because those are
	// different claims: generated output can be missing a type the metadata has, and
	// that would be a bug here rather than an absence upstream.
	//
	//	go run ./cmd/inspect --dir metadata/winmd --search PagerControl
	//	0 types
	//
	// The same command finds 44 types for TabView, 16 for PipsPager, 14 for SelectorBar
	// and 13 for BreadcrumbBar, all of which this file uses. And ingest projects 77 of
	// 77 namespaces and 4,374 types, so nothing is being dropped between the two.
	const pagerReason = "PagerControl is not a Windows App SDK type; it was a WinUI " +
		"experiment that never shipped, so no namespace in the committed metadata " +
		"defines it"
	register(Page{Control: "PagerControl", Name: "PagerControlPage", Unmappable: pagerReason})
	register(Page{Control: "PagerControl", Name: "PagerControlAxeTestPage", Unmappable: pagerReason})
}

// newTab builds one TabViewItem with a header and some content.
func newTab(header, body string, closable bool) (*uixaml.TabViewItem, error) {
	tab, err := uixaml.NewTabViewItem()
	if err != nil {
		return nil, err
	}
	boxed, err := app.Box(header)
	if err != nil {
		return nil, err
	}
	err = tab.SetHeader(boxed)
	boxed.Release()
	if err != nil {
		return nil, err
	}
	if err := tab.SetIsClosable(closable); err != nil {
		return nil, err
	}

	content, err := label(body)
	if err != nil {
		return nil, err
	}
	if err := app.With(content.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(tab.AsContentControl, func(control *uixaml.IContentControl) error {
			return control.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}
	return tab, nil
}

// newTabView builds a TabView holding count tabs.
func newTabView(count int, closable bool) (*uixaml.TabView, error) {
	view, err := uixaml.NewTabView()
	if err != nil {
		return nil, err
	}
	items, err := view.TabItems()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	for i := 1; i <= count; i++ {
		tab, err := newTab(fmt.Sprintf("Tab %d", i),
			fmt.Sprintf("Content of tab %d", i), closable)
		if err != nil {
			return nil, err
		}
		err = items.Append(inspectableOf(&tab.ITabViewItem))
		tab.Release()
		if err != nil {
			return nil, err
		}
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(280), frame.SetWidth(520))
	}); err != nil {
		return nil, err
	}
	return view, nil
}

func buildTabViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newTabView(4, true)
	if err != nil {
		return nil, err
	}
	if err := view.SetIsAddTabButtonVisible(true); err != nil {
		return nil, err
	}

	// AddTabButtonClick is the seam an application fills in: the control draws the
	// button and raises the event, and adding the tab is the caller's business.
	added := 0
	if _, err := app.On(view.AddAddTabButtonClick,
		uixaml.NewTypedEventHandlerOfTabViewAndObject,
		func(sender *uixaml.ITabView, _ *syswinrt.IInspectable) {
			added++
			tab, err := newTab(fmt.Sprintf("Added %d", added), "A tab added at run time", true)
			if err != nil {
				return
			}
			defer tab.Release()
			items, err := sender.TabItems()
			if err != nil {
				return
			}
			defer items.Release()
			_ = items.Append(inspectableOf(&tab.ITabViewItem))
		}); err != nil {
		return nil, err
	}

	caption, err := label("TabView with the add button; tabs are closable.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TabViewAxeTestPage: the control in the states an accessibility scan wants to see.
func buildTabViewAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, closable := range []bool{true, false} {
		caption, err := label(map[bool]string{
			true: "Closable tabs", false: "Non-closable tabs"}[closable])
		if err != nil {
			return nil, err
		}
		view, err := newTabView(3, closable)
		if err != nil {
			return nil, err
		}
		if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(200)
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

// TabViewSizingPage: the tab width modes, which is what the source varies.
func buildTabViewSizingPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	modes := []struct {
		caption string
		value   uixaml.TabViewWidthMode
	}{
		{"Equal", uixaml.TabViewWidthModeEqual},
		{"SizeToContent", uixaml.TabViewWidthModeSizeToContent},
		{"Compact", uixaml.TabViewWidthModeCompact},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range modes {
		caption, err := label("TabWidthMode " + mode.caption)
		if err != nil {
			return nil, err
		}
		view, err := newTabView(5, true)
		if err != nil {
			return nil, err
		}
		if err := app.All(
			view.SetTabWidthMode(mode.value),
			app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return frame.SetHeight(170)
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

// TabViewTabClosingBehaviorPage: closing is a REQUEST, not an action.
//
// TabCloseRequested fires and the tab stays until the handler removes it. That is the
// whole design — it is what lets an application prompt before discarding unsaved work —
// and a handler that forgets to remove the tab produces a close button that does nothing,
// which is the bug this page exists to catch.
func buildTabViewTabClosingBehaviorPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newTabView(5, true)
	if err != nil {
		return nil, err
	}

	status, err := label("Close a tab: the request is handled, then the tab is removed.")
	if err != nil {
		return nil, err
	}
	closed := 0
	if _, err := app.On(view.AddTabCloseRequested,
		uixaml.NewTypedEventHandlerOfTabViewAndTabViewTabCloseRequestedEventArgs,
		func(sender *uixaml.ITabView, args *uixaml.ITabViewTabCloseRequestedEventArgs) {
			tab, err := args.Tab()
			if err != nil {
				_ = status.SetText("Reading the tab failed: " + err.Error())
				return
			}
			defer tab.Release()
			items, err := sender.TabItems()
			if err != nil {
				return
			}
			defer items.Release()

			// IndexOf then RemoveAt: the args carry the tab, not its position. The
			// index is an OUT parameter and the bool is the return, which is the
			// shape every WinRT "try" method has.
			var index uint32
			found, err := items.IndexOf(inspectableOf(tab), &index)
			if err != nil || !found {
				_ = status.SetText("The tab was not found in TabItems.")
				return
			}
			if err := items.RemoveAt(index); err != nil {
				_ = status.SetText("Removing the tab failed: " + err.Error())
				return
			}
			closed++
			_ = status.SetText(fmt.Sprintf("%d tab(s) closed.", closed))
		}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TabViewTabItemsSourcePage: tabs from a bound collection rather than added by hand.
func buildTabViewTabItemsSourcePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewTabView()
	if err != nil {
		return nil, err
	}
	template, err := dataTemplate(
		`<DataTemplate><TabViewItem Header="{Binding}"><TextBlock Text="{Binding}" Margin="12"/></TabViewItem></DataTemplate>`)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	if err := view.SetTabItemTemplate(template); err != nil {
		return nil, err
	}

	source, err := itemsSource(numbered("Bound tab", 5))
	if err != nil {
		return nil, err
	}
	if err := view.SetTabItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(260), frame.SetWidth(520))
	}); err != nil {
		return nil, err
	}

	caption, err := label("Tabs from TabItemsSource with a TabItemTemplate.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// MultipleTabViewPage: two independent TabViews, which is how the source checks that one
// does not drive the other's selection.
func buildMultipleTabViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for i := 1; i <= 2; i++ {
		caption, err := label(fmt.Sprintf("TabView %d", i))
		if err != nil {
			return nil, err
		}
		view, err := newTabView(3, true)
		if err != nil {
			return nil, err
		}
		if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(180)
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

// BreadcrumbBarPage: a path, and the click that truncates it.
//
// The bar renders whatever collection it is given and reports the INDEX that was clicked;
// shortening the path is the application's job. That is why the page rebinds rather than
// mutating — app.ItemsSource has no mutation API — and the same shape an application
// would use with an observable collection.
func buildBreadcrumbBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	full := []string{"Home", "Documents", "Projects", "go-bindings", "examples", "gallery"}

	bar, err := uixaml.NewBreadcrumbBar()
	if err != nil {
		return nil, err
	}
	current, err := itemsSource(full)
	if err != nil {
		return nil, err
	}
	if err := bar.SetItemsSource(current.Inspectable()); err != nil {
		current.Close()
		return nil, err
	}

	status, err := label("Click a crumb to truncate the path there.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(bar.AddItemClicked,
		uixaml.NewTypedEventHandlerOfBreadcrumbBarAndBreadcrumbBarItemClickedEventArgs,
		func(sender *uixaml.IBreadcrumbBar, args *uixaml.IBreadcrumbBarItemClickedEventArgs) {
			index, err := args.Index()
			if err != nil {
				return
			}
			next, err := itemsSource(full[:index+1])
			if err != nil {
				_ = status.SetText("Rebuilding the path failed: " + err.Error())
				return
			}
			if err := sender.SetItemsSource(next.Inspectable()); err != nil {
				next.Close()
				return
			}
			if current != nil {
				current.Close()
			}
			current = next
			_ = status.SetText("Path is now: " + full[index])
		}); err != nil {
		return nil, err
	}

	reset, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Reset the path", func() {
			next, err := itemsSource(full)
			if err != nil {
				return
			}
			if err := bar.SetItemsSource(next.Inspectable()); err != nil {
				next.Close()
				return
			}
			if current != nil {
				current.Close()
			}
			current = next
			_ = status.SetText("Click a crumb to truncate the path there.")
		})
	})
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, bar.AsUIElement, reset.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildBreadcrumbBarAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, path := range [][]string{
		{"Root"},
		{"Root", "Child"},
		{"Root", "Child", "Grandchild", "Great-grandchild", "Deeper still"},
	} {
		bar, err := uixaml.NewBreadcrumbBar()
		if err != nil {
			return nil, err
		}
		source, err := itemsSource(path)
		if err != nil {
			return nil, err
		}
		if err := bar.SetItemsSource(source.Inspectable()); err != nil {
			source.Close()
			return nil, err
		}
		children = append(children, bar.AsUIElement)
	}
	caption, err := label("BreadcrumbBar at one, two and five levels")
	if err != nil {
		return nil, err
	}
	panel, err := stack(12, append([]func() (*uixaml.IUIElement, error){caption.AsUIElement}, children...)...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// PipsPagerPage: the pager in both orientations and each button visibility.
func buildPipsPagerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	visibilities := []struct {
		caption string
		value   uixaml.PipsPagerButtonVisibility
	}{
		{"Visible", uixaml.PipsPagerButtonVisibilityVisible},
		{"VisibleOnPointerOver", uixaml.PipsPagerButtonVisibilityVisibleOnPointerOver},
		{"Collapsed", uixaml.PipsPagerButtonVisibilityCollapsed},
	}

	status, err := label("Selected page: 1")
	if err != nil {
		return nil, err
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, orientation := range []uixaml.Orientation{
		uixaml.OrientationHorizontal, uixaml.OrientationVertical,
	} {
		for _, visibility := range visibilities {
			caption, err := label(fmt.Sprintf("%v, buttons %s", orientation, visibility.caption))
			if err != nil {
				return nil, err
			}
			pager, err := uixaml.NewPipsPager()
			if err != nil {
				return nil, err
			}
			if err := app.All(
				pager.SetNumberOfPages(8),
				pager.SetMaxVisiblePips(5),
				pager.SetOrientation(orientation),
				pager.SetPreviousButtonVisibility(visibility.value),
				pager.SetNextButtonVisibility(visibility.value),
			); err != nil {
				return nil, err
			}
			if _, err := app.On(pager.AddSelectedIndexChanged,
				uixaml.NewTypedEventHandlerOfPipsPagerAndPipsPagerSelectedIndexChangedEventArgs,
				func(sender *uixaml.IPipsPager, _ *uixaml.IPipsPagerSelectedIndexChangedEventArgs) {
					index, err := sender.SelectedPageIndex()
					if err == nil {
						_ = status.SetText(fmt.Sprintf("Selected page: %d", index+1))
					}
				}); err != nil {
				return nil, err
			}
			children = append(children, caption.AsUIElement, pager.AsUIElement)
		}
	}

	panel, err := stack(10, append([]func() (*uixaml.IUIElement, error){status.AsUIElement}, children...)...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func buildPipsPagerAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	pager, err := uixaml.NewPipsPager()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		pager.SetNumberOfPages(5),
		pager.SetPreviousButtonVisibility(uixaml.PipsPagerButtonVisibilityVisible),
		pager.SetNextButtonVisibility(uixaml.PipsPagerButtonVisibilityVisible),
	); err != nil {
		return nil, err
	}
	caption, err := label("PipsPager with both navigation buttons visible")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, pager.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// newSelectorBar builds a SelectorBar over the given captions, selecting the first.
func newSelectorBar(captions []string) (*uixaml.SelectorBar, error) {
	bar, err := uixaml.NewSelectorBar()
	if err != nil {
		return nil, err
	}
	items, err := bar.Items()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	var first *uixaml.ISelectorBarItem
	for index, caption := range captions {
		item, err := uixaml.NewSelectorBarItem()
		if err != nil {
			return nil, err
		}
		if err := item.SetText(caption); err != nil {
			item.Release()
			return nil, err
		}
		entry, err := item.AsSelectorBarItem()
		item.Release()
		if err != nil {
			return nil, err
		}
		if err := items.Append(entry); err != nil {
			entry.Release()
			return nil, err
		}
		if index == 0 {
			first = entry
		} else {
			entry.Release()
		}
	}
	if first != nil {
		err = bar.SetSelectedItem(first)
		first.Release()
		if err != nil {
			return nil, err
		}
	}
	return bar, nil
}

// SelectorBarPage: the control, and the selection it reports.
func buildSelectorBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	bar, err := newSelectorBar([]string{"All", "Recent", "Shared", "Favourites"})
	if err != nil {
		return nil, err
	}
	status, err := label("Selected: All")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(bar.AddSelectionChanged,
		uixaml.NewTypedEventHandlerOfSelectorBarAndSelectorBarSelectionChangedEventArgs,
		func(sender *uixaml.ISelectorBar, _ *uixaml.ISelectorBarSelectionChangedEventArgs) {
			item, err := sender.SelectedItem()
			if err != nil || item == nil {
				_ = status.SetText("Nothing selected.")
				return
			}
			defer item.Release()
			text, err := item.Text()
			if err != nil {
				return
			}
			_ = status.SetText("Selected: " + text)
		}); err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, bar.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// SelectorBarSamplePage: the bar driving what is shown beneath it, which is the whole
// point of the control.
func buildSelectorBarSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	sections := []string{"Overview", "Details", "History"}
	bar, err := newSelectorBar(sections)
	if err != nil {
		return nil, err
	}

	host, err := uixaml.NewContentControl()
	if err != nil {
		return nil, err
	}
	swap := func(name string) {
		body, err := label("The " + name + " section.")
		if err != nil {
			return
		}
		defer body.Release()
		_ = app.With(body.AsUIElement, func(element *uixaml.IUIElement) error {
			return app.With(host.AsContentControl, func(content *uixaml.IContentControl) error {
				return content.SetContent(inspectableOf(element))
			})
		})
	}
	swap(sections[0])

	if _, err := app.On(bar.AddSelectionChanged,
		uixaml.NewTypedEventHandlerOfSelectorBarAndSelectorBarSelectionChangedEventArgs,
		func(sender *uixaml.ISelectorBar, _ *uixaml.ISelectorBarSelectionChangedEventArgs) {
			item, err := sender.SelectedItem()
			if err != nil || item == nil {
				return
			}
			defer item.Release()
			if text, err := item.Text(); err == nil {
				swap(text)
			}
		}); err != nil {
		return nil, err
	}

	panel, err := stack(12, bar.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// SelectorBarSummaryPage: the bar at a few lengths, plus one with icons.
func buildSelectorBarSummaryPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, captions := range [][]string{
		{"One", "Two"},
		{"All", "Recent", "Shared"},
		{"Alpha", "Bravo", "Charlie", "Delta", "Echo"},
	} {
		bar, err := newSelectorBar(captions)
		if err != nil {
			return nil, err
		}
		children = append(children, bar.AsUIElement)
	}

	// One with icons, since Icon is the other half of SelectorBarItem's surface.
	iconBar, err := uixaml.NewSelectorBar()
	if err != nil {
		return nil, err
	}
	items, err := iconBar.Items()
	if err != nil {
		return nil, err
	}
	defer items.Release()
	for _, entry := range []struct{ text, glyph string }{
		{"Home", ""}, {"Search", ""}, {"Settings", ""},
	} {
		item, err := uixaml.NewSelectorBarItem()
		if err != nil {
			return nil, err
		}
		icon, err := uixaml.NewFontIcon()
		if err != nil {
			item.Release()
			return nil, err
		}
		err = icon.SetGlyph(entry.glyph)
		if err == nil {
			var element *uixaml.IIconElement
			element, err = icon.AsIconElement()
			if err == nil {
				err = app.All(item.SetText(entry.text), item.SetIcon(element))
				element.Release()
			}
		}
		icon.Release()
		if err != nil {
			item.Release()
			return nil, err
		}
		bound, err := item.AsSelectorBarItem()
		item.Release()
		if err != nil {
			return nil, err
		}
		err = items.Append(bound)
		bound.Release()
		if err != nil {
			return nil, err
		}
	}
	children = append(children, iconBar.AsUIElement)

	caption, err := label("SelectorBar at three lengths, then one with icons")
	if err != nil {
		return nil, err
	}
	panel, err := stack(12, append([]func() (*uixaml.IUIElement, error){caption.AsUIElement}, children...)...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}
