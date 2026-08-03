//go:build windows && amd64

package pages

// The NavigationView pages.
//
// Sources: controls/dev/NavigationView/TestUI/*Page.xaml
//
// NavigationView is the largest single control in the SDK by surface — 54 members on its
// primary interface alone — because it is really three layouts behind one API. The pane
// sits on the left, collapses to a strip of icons, or moves to the top, and which of those
// you get is decided by PaneDisplayMode and by the control's own width thresholds.
//
// Several sources are named for the Windows release whose behaviour they pin (RS3, RS4).
// Those releases are long past; what the pages still test is the property that behaviour
// became — which is what ports.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

// setPaneMode changes PaneDisplayMode, which lives on INavigationView2.
func setPaneMode(view *uixaml.NavigationView, mode uixaml.NavigationViewPaneDisplayMode) error {
	return app.With(view.AsNavigationView2, func(v2 *uixaml.INavigationView2) error {
		return v2.SetPaneDisplayMode(mode)
	})
}

func init() {
	register(Page{Control: "NavigationView", Name: "NavigationViewPage", Build: buildNavigationViewPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewMinimalPage", Build: buildNavigationViewMinimalPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewInitPage", Build: buildNavigationViewInitPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewAxeTestPage", Build: buildNavigationViewAxeTestPage, Inert: axeReason})
	register(Page{Control: "NavigationView", Name: "NavigationViewTopNavPage", Build: buildNavigationViewTopNavPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewTopNavOnlyPage", Build: buildNavigationViewTopNavOnlyPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewTopNavOverflowButtonPage", Build: buildNavigationViewTopNavOverflowButtonPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewTopNavStorePage", Build: buildNavigationViewTopNavStorePage})
	register(Page{Control: "NavigationView", Name: "NavigationViewIsPaneOpenPage", Build: buildNavigationViewIsPaneOpenPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewCompactPaneLengthTestPage", Build: buildNavigationViewCompactPaneLengthTestPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewStretchPage", Build: buildNavigationViewStretchPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewMenuItemStretchPage", Build: buildNavigationViewMenuItemStretchPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewMenuItemsSourcePage", Build: buildNavigationViewMenuItemsSourcePage})
	register(Page{Control: "NavigationView", Name: "NavigationViewItemTemplatePage", Build: buildNavigationViewItemTemplatePage})
	register(Page{Control: "NavigationView", Name: "NavigationViewCustomMenuItemPage", Build: buildNavigationViewCustomMenuItemPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewSelectedItemEdgeCasePage", Build: buildNavigationViewSelectedItemEdgeCasePage})
	register(Page{Control: "NavigationView", Name: "NavigationViewAnimationPage", Build: buildNavigationViewAnimationPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewCustomThemeResourcesPage", Build: buildNavigationViewCustomThemeResourcesPage})
	register(Page{Control: "NavigationView", Name: "NavigationViewRS3Page", Build: buildNavigationViewRS3Page})
	register(Page{Control: "NavigationView", Name: "NavigationViewRS4Page", Build: buildNavigationViewRS4Page})
	register(Page{Control: "NavigationView", Name: "PaneFooterTestPage", Build: buildPaneFooterTestPage})
	register(Page{Control: "NavigationView", Name: "PaneLayoutTestPage", Build: buildPaneLayoutTestPage})
}

// navItem builds one menu item with an optional icon glyph.
func navItem(content, glyph string) (*uixaml.NavigationViewItem, error) {
	item, err := uixaml.NewNavigationViewItem()
	if err != nil {
		return nil, err
	}
	boxed, err := app.Box(content)
	if err != nil {
		return nil, err
	}
	err = app.With(item.AsContentControl, func(control *uixaml.IContentControl) error {
		return control.SetContent(boxed)
	})
	boxed.Release()
	if err != nil {
		return nil, err
	}

	if glyph != "" {
		icon, err := uixaml.NewFontIcon()
		if err != nil {
			return nil, err
		}
		defer icon.Release()
		if err := icon.SetGlyph(glyph); err != nil {
			return nil, err
		}
		element, err := icon.AsIconElement()
		if err != nil {
			return nil, err
		}
		defer element.Release()
		if err := item.SetIcon(element); err != nil {
			return nil, err
		}
	}
	return item, nil
}

// navEntry is one row of a pane: an item, a header or a separator.
type navEntry struct {
	content   string
	glyph     string
	header    bool
	separator bool
}

// newNavigationView builds a NavigationView with the given pane entries and a content
// host that follows the selection.
func newNavigationView(entries []navEntry, mode uixaml.NavigationViewPaneDisplayMode,
	width, height float64,
) (*uixaml.NavigationView, error) {
	view, err := uixaml.NewNavigationView()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(view.AsNavigationView2, func(v2 *uixaml.INavigationView2) error {
			// PaneDisplayMode and the back button live on INavigationView2. The
			// interface split is chronological, not conceptual: v2 is where the pane
			// modes arrived, so a page about them needs the accessor.
			return v2.SetPaneDisplayMode(mode)
		}),
		view.SetIsSettingsVisible(true),
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(width), frame.SetHeight(height))
		}),
	); err != nil {
		return nil, err
	}

	items, err := view.MenuItems()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	for _, entry := range entries {
		var boxed *syswinrt.IInspectable
		switch {
		case entry.separator:
			separator, err := uixaml.NewNavigationViewItemSeparator()
			if err != nil {
				return nil, err
			}
			boxed = inspectableOf(&separator.INavigationViewItemSeparator)
		case entry.header:
			header, err := uixaml.NewNavigationViewItemHeader()
			if err != nil {
				return nil, err
			}
			content, err := app.Box(entry.content)
			if err != nil {
				return nil, err
			}
			err = app.With(header.AsContentControl, func(control *uixaml.IContentControl) error {
				return control.SetContent(content)
			})
			content.Release()
			if err != nil {
				return nil, err
			}
			boxed = inspectableOf(&header.INavigationViewItemHeader)
		default:
			item, err := navItem(entry.content, entry.glyph)
			if err != nil {
				return nil, err
			}
			boxed = inspectableOf(&item.INavigationViewItem)
		}
		if err := items.Append(boxed); err != nil {
			return nil, err
		}
	}

	// The content host, kept in step with the selection. Without this a NavigationView
	// is a pane with nothing behind it, which hides the half of the control that
	// matters.
	host, err := uixaml.NewContentControl()
	if err != nil {
		return nil, err
	}
	show := func(text string) {
		body, err := label(text)
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
	show("Select something in the pane.")

	if _, err := app.On(view.AddSelectionChanged,
		uixaml.NewTypedEventHandlerOfNavigationViewAndNavigationViewSelectionChangedEventArgs,
		func(_ *uixaml.INavigationView, args *uixaml.INavigationViewSelectionChangedEventArgs) {
			settings, err := args.IsSettingsSelected()
			if err == nil && settings {
				show("The settings page.")
				return
			}
			selected, err := args.SelectedItem()
			if err != nil || selected == nil {
				return
			}
			defer selected.Release()
			// The selected item is the NavigationViewItem, so its Content is what was
			// put there — reached through IContentControl, not through the args.
			content, err := winrtQueryContentControl(selected)
			if err != nil {
				show("Selected an item whose content could not be read.")
				return
			}
			defer content.Release()
			value, err := content.Content()
			if err != nil || value == nil {
				return
			}
			defer value.Release()
			show("Selected: " + app.UnboxOr(value, "(not a string)"))
		}); err != nil {
		return nil, err
	}

	// NavigationView is a ContentControl, so its content is IInspectable.
	if err := app.With(host.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(view.AsContentControl, func(control *uixaml.IContentControl) error {
			return control.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}
	return view, nil
}

// winrtQueryContentControl views a menu item as the ContentControl it is.
func winrtQueryContentControl(value *syswinrt.IInspectable) (*uixaml.IContentControl, error) {
	return winrt.QueryInterface[uixaml.IContentControl](
		unsafe.Pointer(value), &uixaml.IID_IContentControl)
}

// sampleNav is the pane most of these pages use.
func sampleNav() []navEntry {
	return []navEntry{
		{content: "Main", header: true},
		{content: "Home", glyph: ""},
		{content: "Documents", glyph: ""},
		{content: "Pictures", glyph: ""},
		{separator: true},
		{content: "More", header: true},
		{content: "Downloads", glyph: ""},
		{content: "Recent", glyph: ""},
	}
}

func buildNavigationViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeAuto, 620, 380)
	if err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

// NavigationViewMinimalPage: the pane collapsed to the toggle button only.
func buildNavigationViewMinimalPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(),
		uixaml.NavigationViewPaneDisplayModeLeftMinimal, 620, 380)
	if err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

// NavigationViewInitPage checks the properties an application sets BEFORE the control is
// in the tree survive being loaded, which is the class of bug that produces a pane that
// silently reverts to its defaults.
func buildNavigationViewInitPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 340)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		view.SetIsPaneOpen(false),
		view.SetOpenPaneLength(280),
		view.SetIsSettingsVisible(false),
	); err != nil {
		return nil, err
	}

	status, err := label("Set before load; the values below are read after it.")
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				open, _ := view.IsPaneOpen()
				length, _ := view.OpenPaneLength()
				settings, _ := view.IsSettingsVisible()
				_ = status.SetText(fmt.Sprintf(
					"After load: IsPaneOpen=%v, OpenPaneLength=%.0f, IsSettingsVisible=%v",
					open, length, settings))
			})
		return addErr
	}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildNavigationViewAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 380)
	if err != nil {
		return nil, err
	}
	if err := view.SetIsPaneOpen(true); err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

func buildNavigationViewTopNavPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeTop, 620, 340)
	if err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

// NavigationViewTopNavOnlyPage: top mode with the toggle button suppressed, so there is
// no way back to a left pane.
func buildNavigationViewTopNavOnlyPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeTop, 620, 340)
	if err != nil {
		return nil, err
	}
	if err := view.SetIsPaneToggleButtonVisible(false); err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

// NavigationViewTopNavOverflowButtonPage: more items than fit, so the overflow menu
// appears. The control decides what overflows from its own width, which is why the view
// here is deliberately narrow.
func buildNavigationViewTopNavOverflowButtonPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var entries []navEntry
	for _, name := range numbered("Section", 12) {
		entries = append(entries, navEntry{content: name, glyph: ""})
	}
	view, err := newNavigationView(entries, uixaml.NavigationViewPaneDisplayModeTop, 460, 320)
	if err != nil {
		return nil, err
	}
	caption, err := label("Twelve items in a narrow top pane: the rest go to the overflow menu.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NavigationViewTopNavStorePage is the source's realistic top-nav page.
func buildNavigationViewTopNavStorePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	entries := []navEntry{
		{content: "Home", glyph: ""},
		{content: "Apps", glyph: ""},
		{content: "Games", glyph: ""},
		{content: "Films & TV", glyph: ""},
		{content: "Library", glyph: ""},
	}
	view, err := newNavigationView(entries, uixaml.NavigationViewPaneDisplayModeTop, 620, 360)
	if err != nil {
		return nil, err
	}
	header, err := app.Box("Store")
	if err != nil {
		return nil, err
	}
	defer header.Release()
	if err := view.SetHeader(header); err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

// NavigationViewIsPaneOpenPage: the pane opened and closed from outside the control.
func buildNavigationViewIsPaneOpenPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 320)
	if err != nil {
		return nil, err
	}
	status, err := label("IsPaneOpen: true")
	if err != nil {
		return nil, err
	}
	refresh := func() {
		if open, err := view.IsPaneOpen(); err == nil {
			_ = status.SetText(fmt.Sprintf("IsPaneOpen: %v", open))
		}
	}
	// PaneOpening and PaneClosing fire for the control's own toggle as well as for the
	// buttons below, which is what makes the readout trustworthy.
	if err := app.With(view.AsNavigationView2, func(v2 *uixaml.INavigationView2) error {
		if _, err := app.On(v2.AddPaneOpening, uixaml.NewTypedEventHandlerOfNavigationViewAndObject,
			func(_ *uixaml.INavigationView, _ *syswinrt.IInspectable) { refresh() }); err != nil {
			return err
		}
		_, err := app.On(v2.AddPaneClosing,
			uixaml.NewTypedEventHandlerOfNavigationViewAndNavigationViewPaneClosingEventArgs,
			func(_ *uixaml.INavigationView, _ *uixaml.INavigationViewPaneClosingEventArgs) { refresh() })
		return err
	}); err != nil {
		return nil, err
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Open the pane", func() { _ = view.SetIsPaneOpen(true); refresh() })
		},
		func() (*uixaml.Button, error) {
			return button("Close the pane", func() { _ = view.SetIsPaneOpen(false); refresh() })
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

// NavigationViewCompactPaneLengthTestPage: the width of the closed pane.
func buildNavigationViewCompactPaneLengthTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, length := range []float64{48, 72, 96} {
		caption, err := label(fmt.Sprintf("CompactPaneLength %.0f", length))
		if err != nil {
			return nil, err
		}
		view, err := newNavigationView(sampleNav(),
			uixaml.NavigationViewPaneDisplayModeLeftCompact, 560, 220)
		if err != nil {
			return nil, err
		}
		if err := view.SetCompactPaneLength(length); err != nil {
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

// NavigationViewStretchPage and NavigationViewMenuItemStretchPage: how the content and
// the items fill the space they are given.
func buildNavigationViewStretchPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 640, 400)
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(
			frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentStretch),
			frame.SetVerticalAlignment(uixaml.VerticalAlignmentStretch),
		)
	}); err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

func buildNavigationViewMenuItemStretchPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	entries := []navEntry{
		{content: "A short one", glyph: ""},
		{content: "An item with a considerably longer label than the others", glyph: ""},
		{content: "Medium length item", glyph: ""},
	}
	view, err := newNavigationView(entries, uixaml.NavigationViewPaneDisplayModeLeft, 640, 320)
	if err != nil {
		return nil, err
	}
	if err := view.SetOpenPaneLength(320); err != nil {
		return nil, err
	}
	caption, err := label("Items of differing label lengths in a 320px pane.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NavigationViewMenuItemsSourcePage: the pane bound rather than built.
//
// MenuItemsSource and MenuItems are mutually exclusive in the same way TreeView's two
// models are: binding a source makes MenuItems the control's own business.
func buildNavigationViewMenuItemsSourcePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewNavigationView()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		setPaneMode(view, uixaml.NavigationViewPaneDisplayModeLeft),
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(620), frame.SetHeight(340))
		}),
	); err != nil {
		return nil, err
	}

	source, err := itemsSource(numbered("Bound item", 6))
	if err != nil {
		return nil, err
	}
	if err := view.SetMenuItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, err
	}

	items, err := view.MenuItems()
	if err != nil {
		return nil, err
	}
	defer items.Release()
	size, err := items.Size()
	if err != nil {
		return nil, err
	}
	status, err := label(fmt.Sprintf(
		"Bound through MenuItemsSource. MenuItems reports %d — the two are alternatives, "+
			"not layers, exactly as TreeView's node and ItemsSource models are.", size))
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NavigationViewItemTemplatePage: a bound pane with a template for each item.
func buildNavigationViewItemTemplatePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewNavigationView()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		setPaneMode(view, uixaml.NavigationViewPaneDisplayModeLeft),
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(620), frame.SetHeight(340))
		}),
	); err != nil {
		return nil, err
	}

	template, err := dataTemplate(
		`<DataTemplate><NavigationViewItem Content="{Binding}"><NavigationViewItem.Icon>` +
			`<FontIcon Glyph="&#xE8A5;"/></NavigationViewItem.Icon></NavigationViewItem></DataTemplate>`)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	if err := view.SetMenuItemTemplate(template); err != nil {
		return nil, err
	}

	source, err := itemsSource(numbered("Templated", 6))
	if err != nil {
		return nil, err
	}
	if err := view.SetMenuItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, err
	}
	return view.AsUIElement()
}

// NavigationViewCustomMenuItemPage: an item that is not a NavigationViewItem.
//
// MenuItems is a collection of Object, so anything can go in it — the source puts a
// Button there. It renders, and it is NOT selectable, which is the distinction the page
// exists to draw.
func buildNavigationViewCustomMenuItemPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView([]navEntry{
		{content: "An ordinary item", glyph: ""},
	}, uixaml.NavigationViewPaneDisplayModeLeft, 620, 320)
	if err != nil {
		return nil, err
	}

	items, err := view.MenuItems()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	custom, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(custom.AsContentControl, "A Button in the pane"); err != nil {
		return nil, err
	}
	if err := items.Append(inspectableOf(&custom.IButton)); err != nil {
		return nil, err
	}

	caption, err := label("MenuItems holds Object, so a Button can go in it — and it " +
		"renders without becoming selectable.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NavigationViewSelectedItemEdgeCasePage: setting the selection to nothing, and to an
// item that is not in the pane.
func buildNavigationViewSelectedItemEdgeCasePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 300)
	if err != nil {
		return nil, err
	}
	status, err := label("Try the edge cases below.")
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Clear the selection", func() {
				if err := view.SetSelectedItem(nil); err != nil {
					_ = status.SetText("Clearing failed: " + err.Error())
					return
				}
				_ = status.SetText("SelectedItem set to nil; the control accepted it.")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Select an item not in the pane", func() {
				stranger, err := navItem("Not in this pane", "")
				if err != nil {
					return
				}
				defer stranger.Release()
				err = view.SetSelectedItem(inspectableOf(&stranger.INavigationViewItem))
				if err != nil {
					_ = status.SetText("Rejected, with: " + err.Error())
					return
				}
				_ = status.SetText("Accepted an item that is not in MenuItems.")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Select settings", func() {
				settings, err := view.SettingsItem()
				if err != nil || settings == nil {
					_ = status.SetText("There is no settings item.")
					return
				}
				defer settings.Release()
				if err := view.SetSelectedItem(settings); err != nil {
					_ = status.SetText("Selecting settings failed: " + err.Error())
					return
				}
				_ = status.SetText("Settings selected.")
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

// NavigationViewAnimationPage: the pane transition, driven by toggling the mode.
func buildNavigationViewAnimationPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 320)
	if err != nil {
		return nil, err
	}
	modes := []uixaml.NavigationViewPaneDisplayMode{
		uixaml.NavigationViewPaneDisplayModeLeft,
		uixaml.NavigationViewPaneDisplayModeLeftCompact,
		uixaml.NavigationViewPaneDisplayModeLeftMinimal,
		uixaml.NavigationViewPaneDisplayModeTop,
	}
	at := 0
	status, err := label("PaneDisplayMode: Left")
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Next display mode", func() {
			at = (at + 1) % len(modes)
			if err := setPaneMode(view, modes[at]); err != nil {
				_ = status.SetText("Changing the mode failed: " + err.Error())
				return
			}
			_ = status.SetText("PaneDisplayMode: " + modes[at].String())
		})
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NavigationViewCustomThemeResourcesPage overrides the control's brushes.
//
// The source does it with a ResourceDictionary merged into the page. app.Resource reaches
// the same dictionary from Go, so the port sets the values into the control's own
// Resources — which is where a per-control override belongs, and is why it affects this
// NavigationView and no other.
func buildNavigationViewCustomThemeResourcesPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 340)
	if err != nil {
		return nil, err
	}

	status, err := label("Overriding NavigationViewItem brushes on this control only.")
	if err != nil {
		return nil, err
	}
	err = app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		resources, err := frame.Resources()
		if err != nil {
			return err
		}
		defer resources.Release()
		// The map accessor is on the CLASS, not on IResourceDictionary — the dictionary
		// implements IMap<Object, Object> and the projection puts that accessor where
		// the class's interface list is known.
		dictionary := (*uixaml.ResourceDictionary)(unsafe.Pointer(resources))
		lookup, err := dictionary.AsMapOfObjectAndObject()
		if err != nil {
			return err
		}
		defer lookup.Release()

		brush, err := solidBrush(wrtui.Color{A: 255, R: 90, G: 140, B: 200})
		if err != nil {
			return err
		}
		defer brush.Release()
		key, err := app.Box("NavigationViewItemForeground")
		if err != nil {
			return err
		}
		defer key.Release()
		_, err = lookup.Insert(key, inspectableOf(brush))
		return err
	})
	if err != nil {
		_ = status.SetText("Overriding the brush failed: " + err.Error())
	}

	panel, err := stack(8, status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NavigationViewRS3Page and NavigationViewRS4Page pin behaviour introduced in two
// Windows 10 releases. Those releases are long past and the behaviour is now simply the
// control's: RS3 brought the back button, RS4 the pane header and footer.
func buildNavigationViewRS3Page(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 320)
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsNavigationView2, func(v2 *uixaml.INavigationView2) error {
		return app.All(
			v2.SetIsBackButtonVisible(uixaml.NavigationViewBackButtonVisibleVisible),
			v2.SetIsBackEnabled(true),
		)
	}); err != nil {
		return nil, err
	}
	status, err := label("The back button, which is what this page's release added.")
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsNavigationView2, func(v2 *uixaml.INavigationView2) error {
		_, addErr := app.On(v2.AddBackRequested,
			uixaml.NewTypedEventHandlerOfNavigationViewAndNavigationViewBackRequestedEventArgs,
			func(_ *uixaml.INavigationView, _ *uixaml.INavigationViewBackRequestedEventArgs) {
				_ = status.SetText("BackRequested: navigating back is the application's job.")
			})
		return addErr
	}); err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildNavigationViewRS4Page(ready *app.Ready) (*uixaml.IUIElement, error) {
	return paneHeaderAndFooter("The pane header and footer, which is what this page's release added.")
}

// PaneFooterTestPage and PaneLayoutTestPage are both about the parts of the pane that
// are not menu items.
func buildPaneFooterTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return paneHeaderAndFooter("PaneHeader, PaneFooter and FooterMenuItems.")
}

func buildPaneLayoutTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return paneHeaderAndFooter("The pane's full layout: header, items, footer items, footer.")
}

// paneHeaderAndFooter builds the shape those three pages share.
func paneHeaderAndFooter(note string) (*uixaml.IUIElement, error) {
	view, err := newNavigationView(sampleNav(), uixaml.NavigationViewPaneDisplayModeLeft, 620, 380)
	if err != nil {
		return nil, err
	}

	header, err := label("Pane header")
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsNavigationView2, func(v2 *uixaml.INavigationView2) error {
		return app.With(header.AsUIElement, v2.SetPaneHeader)
	}); err != nil {
		return nil, err
	}

	footer, err := label("Pane footer")
	if err != nil {
		return nil, err
	}
	if err := app.With(footer.AsUIElement, view.SetPaneFooter); err != nil {
		return nil, err
	}

	// FooterMenuItems is a separate collection pinned to the bottom of the pane, above
	// PaneFooter — which is why both exist.
	footerItems, err := view.FooterMenuItems()
	if err != nil {
		return nil, err
	}
	defer footerItems.Release()
	for _, entry := range []struct{ content, glyph string }{
		{"Account", ""}, {"Feedback", ""},
	} {
		item, err := navItem(entry.content, entry.glyph)
		if err != nil {
			return nil, err
		}
		err = footerItems.Append(inspectableOf(&item.INavigationViewItem))
		item.Release()
		if err != nil {
			return nil, err
		}
	}

	caption, err := label(note)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}
