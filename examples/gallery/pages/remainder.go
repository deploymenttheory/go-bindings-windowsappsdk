//go:build windows && amd64

package pages

// The last twelve pages: everything the earlier batches did not group.
//
// Sources: controls/dev/{AutoSuggestBox,PersonPicture,SplitView,TitleBar,TwoPaneView,
// SystemBackdropElement,MapControl,Interactions,WebView2}/**/*Page.xaml
//
// With these the gallery covers every *Page.xaml in microsoft/microsoft-ui-xaml.

import (
	"fmt"
	"strings"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "AutoSuggestBox", Name: "AutoSuggestBoxPage", Build: buildAutoSuggestBoxPage})
	register(Page{Control: "PersonPicture", Name: "PersonPicturePage", Build: buildPersonPicturePage})
	register(Page{Control: "SplitView", Name: "SplitViewPage", Build: buildSplitViewPage})
	// TitleBar FAIL-FASTS at layout in this gallery, and the cause is NOT yet known.
	//
	// What was measured: a probe that creates a TitleBar, sets Title, Subtitle,
	// IconSource, IsBackButtonVisible and IsPaneToggleButtonVisible, sets a RightHeader,
	// registers BackRequested and PaneToggleRequested, wraps it in a StackPanel, puts it
	// in the window and activates — SURVIVES. The same control as a gallery page dies at
	// the layout pass with STATUS_FAIL_FAST_EXCEPTION (0xc0000602), consistently.
	//
	// So the control and every call on it are fine, and something about the way a page is
	// hosted is not. A fail-fast leaves no HRESULT and no restricted error info, so the
	// next step is bisecting the hosting path rather than the control.
	//
	// Recorded rather than shipped, because a page that terminates the process takes the
	// conformance run with it. Recorded rather than explained, because two explanations
	// have already been offered for this page and neither survived — the second appeared
	// to pass only because the staged test binary was stale.
	register(Page{Control: "TitleBar", Name: "TitleBarPage",
		Unmappable: "the control constructs and configures correctly — a standalone " +
			"probe survives every call and arrangement tried — but fail-fasts " +
			"(0xc0000602) at the layout pass when hosted as a gallery page; cause not " +
			"yet established"})
	register(Page{Control: "TwoPaneView", Name: "TwoPaneViewPage", Build: buildTwoPaneViewPage})
	register(Page{Control: "SystemBackdropElement", Name: "SystemBackdropElementPage", Build: buildSystemBackdropElementPage})
	// MapControl FAIL-FASTS at layout without a MapServiceToken, verified rather than
	// assumed: a probe that creates one, reads MapServiceToken (empty), sets a size,
	// puts it in the tree and activates the window dies at the layout pass with
	// STATUS_FAIL_FAST_EXCEPTION (0xc0000602). There is no HRESULT to catch — a
	// fail-fast is the control refusing to continue.
	//
	// The token comes from an Azure Maps account. A gallery cannot embed one, and a page
	// that terminates the process is worse than a page that explains itself, so this is
	// recorded rather than shipped. Everything about the control up to layout works,
	// which is what the probe establishes.
	register(Page{Control: "MapControl", Name: "MapControlPage",
		Unmappable: "MapControl fail-fasts at layout (0xc0000602) without a " +
			"MapServiceToken from an Azure Maps account, which this gallery has no " +
			"credential to embed; the control is present and projected, and every call " +
			"before layout succeeds"})

	register(Page{Control: "Interactions", Name: "ButtonInteractionPage", Build: buildButtonInteractionPage})
	register(Page{Control: "Interactions", Name: "SliderInteractionPage", Build: buildSliderInteractionPage})

	register(Page{Control: "WebView2", Name: "WebView2Page", Build: buildWebView2Page})
	register(Page{Control: "WebView2", Name: "WebView2BasicPage", Build: buildWebView2BasicPage})

	// WebView2CoreObjectsPage is the one page in this family that cannot be ported, and
	// the reason is narrower than the plan assumed.
	//
	// The plan declared all three WebView2 pages permanently unmappable, on the grounds
	// that Microsoft.Web.WebView2.Core has no Go bindings. The namespace really is
	// foreign — it is in ingest.KnownForeignNamespaces and its members are skipped with
	// foreign-type-skipped — but the CONTROL is not. Microsoft.UI.Xaml.Controls.WebView2
	// is ordinary Windows App SDK metadata and its navigation surface is fully
	// projected: Source, NavigateToString, Reload, GoBack, GoForward,
	// ExecuteScriptAsync, EnsureCoreWebView2Async. Two of the three pages need only
	// that, and they are above.
	//
	// What is genuinely absent is the CoreWebView2 object itself and the four navigation
	// events, whose argument types are all Microsoft.Web.WebView2.Core.* — visible in
	// the baseline as event-delegate-unloweable for NavigationStarting,
	// NavigationCompleted, WebMessageReceived and CoreProcessFailed. This page is about
	// exactly those objects, so it is the one that stays.
	register(Page{Control: "WebView2", Name: "WebView2CoreObjectsPage",
		Unmappable: "the page drives the CoreWebView2 object and the navigation events, " +
			"whose types are all Microsoft.Web.WebView2.Core.* — a foreign namespace with " +
			"no Go bindings (foreign-type-skipped, and event-delegate-unloweable for the " +
			"four events). The WebView2 control's own navigation surface IS projected, " +
			"which is why the other two WebView2 pages port"})
}

// AutoSuggestBoxPage: the box, its suggestions, and which event does what.
//
// Three events and they are not interchangeable. TextChanged fires as the user types and
// is where the suggestion list is refilled — and its Reason matters, because the box also
// raises it when the text changes programmatically or from a chosen suggestion, and
// refilling on those causes a loop. SuggestionChosen fires when one is picked.
// QuerySubmitted fires on Enter or on the query button, and is the one that means "go".
func buildAutoSuggestBoxPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	corpus := []string{
		"Aardvark", "Albatross", "Alpaca", "Badger", "Barracuda", "Bison",
		"Camel", "Capybara", "Cheetah", "Dolphin", "Dormouse", "Eagle",
		"Elephant", "Ferret", "Flamingo", "Gazelle", "Giraffe", "Hedgehog",
	}

	box, err := uixaml.NewAutoSuggestBox()
	if err != nil {
		return nil, err
	}
	header, err := app.Box("Animal")
	if err != nil {
		return nil, err
	}
	defer header.Release()
	if err := app.All(
		box.SetHeader(header),
		box.SetPlaceholderText("Start typing"),
		box.SetQueryIcon(nil),
		app.With(box.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetWidth(320)
		}),
	); err != nil {
		return nil, err
	}

	status, err := label("Type to filter; press Enter to submit.")
	if err != nil {
		return nil, err
	}

	// The source held here for the life of the page, replaced on each keystroke.
	var current *app.ItemsSource
	rebind := func(values []string) {
		next, err := itemsSource(values)
		if err != nil {
			return
		}
		// AutoSuggestBox IS an ItemsControl — the suggestion list is its items — so
		// ItemsSource comes from that base rather than from the box own interface.
		if err := app.With(box.AsItemsControl, func(items *uixaml.IItemsControl) error {
			return items.SetItemsSource(next.Inspectable())
		}); err != nil {
			next.Close()
			return
		}
		if current != nil {
			current.Close()
		}
		current = next
	}

	if _, err := app.On(box.AddTextChanged,
		uixaml.NewTypedEventHandlerOfAutoSuggestBoxAndAutoSuggestBoxTextChangedEventArgs,
		func(sender *uixaml.IAutoSuggestBox, args *uixaml.IAutoSuggestBoxTextChangedEventArgs) {
			reason, err := args.Reason()
			if err != nil {
				return
			}
			// Refilling on anything but user input is what makes an AutoSuggestBox
			// loop: choosing a suggestion sets the text, which raises this again.
			if reason != uixaml.AutoSuggestionBoxTextChangeReasonUserInput {
				return
			}
			text, err := sender.Text()
			if err != nil {
				return
			}
			var matched []string
			for _, entry := range corpus {
				if strings.Contains(strings.ToLower(entry), strings.ToLower(text)) {
					matched = append(matched, entry)
				}
			}
			if len(matched) == 0 {
				matched = []string{"No matches"}
			}
			rebind(matched)
			_ = status.SetText(fmt.Sprintf("TextChanged (UserInput): %d suggestion(s)",
				len(matched)))
		}); err != nil {
		return nil, err
	}

	if _, err := app.On(box.AddSuggestionChosen,
		uixaml.NewTypedEventHandlerOfAutoSuggestBoxAndAutoSuggestBoxSuggestionChosenEventArgs,
		func(_ *uixaml.IAutoSuggestBox, args *uixaml.IAutoSuggestBoxSuggestionChosenEventArgs) {
			chosen, err := args.SelectedItem()
			if err != nil || chosen == nil {
				return
			}
			defer chosen.Release()
			_ = status.SetText("SuggestionChosen: " + app.UnboxOr(chosen, "(not a string)"))
		}); err != nil {
		return nil, err
	}

	if _, err := app.On(box.AddQuerySubmitted,
		uixaml.NewTypedEventHandlerOfAutoSuggestBoxAndAutoSuggestBoxQuerySubmittedEventArgs,
		func(_ *uixaml.IAutoSuggestBox, args *uixaml.IAutoSuggestBoxQuerySubmittedEventArgs) {
			text, err := args.QueryText()
			if err != nil {
				return
			}
			_ = status.SetText("QuerySubmitted: " + text)
		}); err != nil {
		return nil, err
	}

	rebind(corpus)

	note, err := label("TextChanged refills the list and is filtered on Reason — " +
		"refilling on anything but UserInput makes the box loop, because choosing a " +
		"suggestion sets the text and raises it again.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, status.AsUIElement, box.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// PersonPicturePage: the fallbacks, in the order the control applies them.
//
// A ProfilePicture wins over Initials, Initials over DisplayName, and IsGroup replaces all
// of it with a group glyph. Showing them side by side is the only way to see the
// precedence, which is what the source page is for.
func buildPersonPicturePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	cases := []struct {
		caption  string
		display  string
		initials string
		group    bool
		badge    int32
		glyph    string
	}{
		{"DisplayName only — initials derived", "Ada Lovelace", "", false, 0, ""},
		{"Initials set explicitly", "Ada Lovelace", "AL", false, 0, ""},
		{"IsGroup — the person is replaced by a group glyph", "Engineering", "", true, 0, ""},
		{"BadgeNumber", "Grace Hopper", "", false, 7, ""},
		{"BadgeGlyph", "Alan Turing", "", false, 0, ""},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range cases {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		picture, err := uixaml.NewPersonPicture()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			picture.SetDisplayName(entry.display),
			picture.SetInitials(entry.initials),
			picture.SetIsGroup(entry.group),
			picture.SetBadgeNumber(entry.badge),
			picture.SetBadgeGlyph(entry.glyph),
			app.With(picture.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return app.All(frame.SetWidth(72), frame.SetHeight(72))
			}),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, picture.AsUIElement)
	}

	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// SplitViewPage: the display modes, which is the whole of the control.
//
// SplitView is NavigationView without the opinions: a pane and a content area, and four
// ways of relating them. Overlay floats the pane above the content; Inline pushes it
// aside; the Compact variants leave a strip showing when closed.
func buildSplitViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	modes := []struct {
		caption string
		value   uixaml.SplitViewDisplayMode
	}{
		{"Overlay — the pane floats over the content", uixaml.SplitViewDisplayModeOverlay},
		{"Inline — the content is pushed aside", uixaml.SplitViewDisplayModeInline},
		{"CompactOverlay — a strip stays visible", uixaml.SplitViewDisplayModeCompactOverlay},
		{"CompactInline", uixaml.SplitViewDisplayModeCompactInline},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range modes {
		caption, err := label(mode.caption)
		if err != nil {
			return nil, err
		}
		view, err := uixaml.NewSplitView()
		if err != nil {
			return nil, err
		}

		pane, err := colouredBand(200, 140, "Pane", bandColours[2])
		if err != nil {
			return nil, err
		}
		content, err := colouredBand(320, 140, "Content", bandColours[4])
		if err != nil {
			return nil, err
		}
		if err := app.All(
			view.SetDisplayMode(mode.value),
			view.SetIsPaneOpen(true),
			view.SetOpenPaneLength(160),
			view.SetCompactPaneLength(48),
			app.With(pane.AsUIElement, view.SetPane),
			app.With(content.AsUIElement, view.SetContent),
			app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return app.All(frame.SetWidth(480), frame.SetHeight(150))
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

// TwoPaneViewPage: the layout for dual-screen and wide windows.
//
// The control decides for itself whether to show one pane or two, from the width against
// MinWideModeWidth and MinTallModeHeight. PanePriority is which one wins when only one
// fits — so the page varies exactly that.
func buildTwoPaneViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	cases := []struct {
		caption  string
		priority uixaml.TwoPaneViewPriority
		width    float64
	}{
		{"Wide enough for both", uixaml.TwoPaneViewPriorityPane1, 520},
		{"Too narrow, Pane1 wins", uixaml.TwoPaneViewPriorityPane1, 240},
		{"Too narrow, Pane2 wins", uixaml.TwoPaneViewPriorityPane2, 240},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range cases {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		view, err := uixaml.NewTwoPaneView()
		if err != nil {
			return nil, err
		}
		one, err := colouredBand(240, 120, "Pane 1", bandColours[0])
		if err != nil {
			return nil, err
		}
		two, err := colouredBand(240, 120, "Pane 2", bandColours[3])
		if err != nil {
			return nil, err
		}
		if err := app.All(
			app.With(one.AsUIElement, view.SetPane1),
			app.With(two.AsUIElement, view.SetPane2),
			view.SetPanePriority(entry.priority),
			view.SetMinWideModeWidth(400),
			app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return app.All(frame.SetWidth(entry.width), frame.SetHeight(130))
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

// SystemBackdropElementPage: an element that carries a backdrop of its own.
//
// A SystemBackdrop is normally a window-level thing — Mica behind the whole application.
// SystemBackdropElement is the seam that lets one region have its own, which is what the
// source page is checking exists.
func buildSystemBackdropElementPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	element, err := uixaml.NewSystemBackdropElement()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		element.SetCornerRadius(uixaml.CornerRadius{
			TopLeft: 12, TopRight: 12, BottomRight: 12, BottomLeft: 12}),
		app.With(element.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(360), frame.SetHeight(200))
		}),
	); err != nil {
		return nil, err
	}

	backdrop, err := element.SystemBackdrop()
	state := "no SystemBackdrop is set, so the element is transparent"
	if err == nil && backdrop != nil {
		backdrop.Release()
		state = "a SystemBackdrop is set"
	}

	note, err := label("SystemBackdropElement: " + state + ".\n\n" +
		"A backdrop is normally window-level — Mica behind the whole application. This " +
		"element is the seam that lets one region carry its own, which is what the " +
		"source page checks exists.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, element.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ButtonInteractionPage and SliderInteractionPage come from controls/dev/Interactions,
// where the test app attaches custom InteractionTracker behaviour to ordinary controls.
//
// The trackers themselves are composition-level and the source builds them in C#. What
// the pages test at the XAML level is that the control still behaves — the button still
// clicks, the slider still moves — with the interaction attached, so that is what ports:
// the control, driven, reporting what it did.
func buildButtonInteractionPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("No interaction yet.")
	if err != nil {
		return nil, err
	}

	clicks := 0
	control, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(control.AsContentControl, "Press me"); err != nil {
		return nil, err
	}
	if err := app.With(control.AsButtonBase, func(base *uixaml.IButtonBase) error {
		if _, err := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				clicks++
				_ = status.SetText(fmt.Sprintf("Click %d", clicks))
			}); err != nil {
			return err
		}
		// The press and release halves, which is what an interaction tracker would be
		// watching, and what the source page distinguishes.
		return nil
	}); err != nil {
		return nil, err
	}
	// Pointer events are UIElement s, not ButtonBase s — pressing is something any
	// element can observe, and only the Click on top of it belongs to a button.
	if err := app.With(control.AsUIElement, func(element *uixaml.IUIElement) error {
		if _, err := app.On(element.AddPointerPressed, uixaml.NewPointerEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IPointerRoutedEventArgs) {
				_ = status.SetText("PointerPressed")
			}); err != nil {
			return err
		}
		_, err := app.On(element.AddPointerReleased, uixaml.NewPointerEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IPointerRoutedEventArgs) {
				_ = status.SetText("PointerReleased")
			})
		return err
	}); err != nil {
		return nil, err
	}

	note, err := label("The source attaches an InteractionTracker, which is built in " +
		"composition rather than XAML. What the page tests at this level is that the " +
		"control still behaves with one attached, so the button is driven and reports.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, status.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildSliderInteractionPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Slider at 50.")
	if err != nil {
		return nil, err
	}
	slider, err := uixaml.NewSlider()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(slider.AsRangeBase, func(base *uixaml.IRangeBase) error {
			return app.All(base.SetMinimum(0), base.SetMaximum(100), base.SetValue(50))
		}),
		app.With(slider.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetWidth(320)
		}),
	); err != nil {
		return nil, err
	}
	if err := app.With(slider.AsRangeBase, func(base *uixaml.IRangeBase) error {
		_, addErr := app.On(base.AddValueChanged, uixaml.NewRangeBaseValueChangedEventHandler,
			func(_ *syswinrt.IInspectable, args *uixaml.IRangeBaseValueChangedEventArgs) {
				value, err := args.NewValue()
				if err != nil {
					return
				}
				_ = status.SetText(fmt.Sprintf("Slider at %.0f", value))
			})
		return addErr
	}); err != nil {
		return nil, err
	}

	panel, err := stack(10, status.AsUIElement, slider.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// newWebView builds a WebView2 showing local HTML.
//
// NavigateToString is used rather than a URL because it needs no network, which keeps the
// page deterministic — and because it exercises the same navigation path.
func newWebView(html string) (*uixaml.WebView2, error) {
	view, err := uixaml.NewWebView2()
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(460), frame.SetHeight(280))
	}); err != nil {
		return nil, err
	}
	// The browser process is started on demand; navigating before it exists fails, so
	// this happens once the element is live.
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				if operation, err := view.EnsureCoreWebView2Async(); err == nil {
					operation.Release()
				}
				_ = view.NavigateToString(html)
			})
		return addErr
	}); err != nil {
		return nil, err
	}
	return view, nil
}

// WebView2Page: the control and its navigation surface.
func buildWebView2Page(ready *app.Ready) (*uixaml.IUIElement, error) {
	const html = `<html><body style="font-family:Segoe UI;padding:24px">
		<h2>WebView2, driven from Go</h2>
		<p>This document was passed to NavigateToString.</p>
		<p><a href="https://example.invalid">A link that will not resolve</a></p>
		</body></html>`

	view, err := newWebView(html)
	if err != nil {
		return nil, err
	}
	status, err := label("Loaded from a string.")
	if err != nil {
		return nil, err
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Reload", func() {
				if err := view.Reload(); err != nil {
					_ = status.SetText("Reload failed: " + err.Error())
					return
				}
				_ = status.SetText("Reloaded.")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Go back", func() {
				can, err := view.CanGoBack()
				if err != nil || !can {
					_ = status.SetText("Nothing to go back to.")
					return
				}
				_ = view.GoBack()
				_ = status.SetText("Went back.")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Run some script", func() {
				operation, err := view.ExecuteScriptAsync("document.title")
				if err != nil {
					_ = status.SetText("ExecuteScriptAsync failed: " + err.Error())
					return
				}
				// Not awaited, for the reason ContentDialogPage records: the completion
				// is delivered on this thread.
				operation.Release()
				_ = status.SetText("Script submitted.")
			})
		},
	)
	if err != nil {
		return nil, err
	}

	note, err := label("The WebView2 CONTROL is fully projected — Source, " +
		"NavigateToString, Reload, GoBack, GoForward, ExecuteScriptAsync. What is not is " +
		"the CoreWebView2 object and the navigation events, whose types come from " +
		"Microsoft.Web.WebView2.Core.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, status.AsUIElement, row.AsUIElement,
		view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// WebView2BasicPage: the smallest possible use, which is what the source's basic page is.
func buildWebView2BasicPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := newWebView(`<html><body style="font-family:Segoe UI;padding:24px">
		<h3>A WebView2 with nothing else on the page.</h3></body></html>`)
	if err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

var _ = wrtui.Color{}
