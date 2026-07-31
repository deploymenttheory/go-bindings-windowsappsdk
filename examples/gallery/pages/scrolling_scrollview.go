//go:build windows && amd64

package pages

// The ScrollView pages.
//
// Sources: controls/dev/ScrollView/TestUI/*Page.xaml
//
// ScrollView is WinUI 3's scrolling control, and it is not ScrollViewer. ScrollViewer is
// the UWP one, still present and still used by ListView's template; ScrollView is built
// on ScrollPresenter and exposes the animated, interruptible view changes that
// ScrollViewer never had — ScrollTo, ScrollBy, AddScrollVelocity, each returning a
// correlation id.

import (
	"fmt"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "ScrollView", Name: "ScrollViewPage", Build: buildScrollViewPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewBlankPage", Build: buildScrollViewBlankPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewsWithSimpleContentsPage", Build: buildScrollViewsWithSimpleContentsPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewDynamicPage", Build: buildScrollViewDynamicPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewWithRTLFlowDirectionPage", Build: buildScrollViewWithRTLFlowDirectionPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewKeyboardAndGamepadNavigationPage", Build: buildScrollViewKeyboardAndGamepadNavigationPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewBringIntoViewPage", Build: buildScrollViewBringIntoViewPage})
	register(Page{Control: "ScrollView", Name: "ScrollViewWithScrollControllersPage", Build: buildScrollViewWithScrollControllersPage})
}

// newScrollView builds a ScrollView of a fixed viewport size around content larger than
// it, which is the starting point of every page here.
func newScrollView(viewportWidth, viewportHeight float64, content func() (*uixaml.IUIElement, error)) (*uixaml.ScrollView, error) {
	view, err := uixaml.NewScrollView()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(viewportWidth), frame.SetHeight(viewportHeight))
		}),
		app.With(content, view.SetContent),
	); err != nil {
		return nil, err
	}
	return view, nil
}

func buildScrollViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return navigationIndex("ScrollView", []string{
		"ScrollViewsWithSimpleContentsPage — several ScrollViews side by side",
		"ScrollViewDynamicPage — exercises the ScrollView API",
		"ScrollViewWithScrollControllersPage — IScrollControllers in the template",
		"ScrollViewWithRTLFlowDirectionPage — right-to-left flow",
		"ScrollViewKeyboardAndGamepadNavigationPage — keyboard and gamepad navigation",
		"ScrollViewBringIntoViewPage — StartBringIntoView",
		"ScrollViewBlankPage — a ScrollView on an otherwise empty page",
	})
}

// ScrollViewBlankPage is a ScrollView and nothing else. It looks like filler and is not:
// it is the page that shows what a ScrollView does with no properties set at all.
func buildScrollViewBlankPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(600, 1200, 6)
	if err != nil {
		return nil, err
	}
	view, err := newScrollView(400, 300, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	return view.AsUIElement()
}

// ScrollViewsWithSimpleContentsPage: the same control around each kind of content, which
// is how the source checks that measurement does not depend on what is inside.
func buildScrollViewsWithSimpleContentsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	contents := []struct {
		caption string
		build   func() (*uixaml.IUIElement, error)
	}{
		{"A single large band", func() (*uixaml.IUIElement, error) {
			band, err := colouredBand(600, 800, "One band", bandColours[0])
			if err != nil {
				return nil, err
			}
			return band.AsUIElement()
		}},
		{"A stack of bands", func() (*uixaml.IUIElement, error) {
			panel, err := bigContent(600, 1200, 6)
			if err != nil {
				return nil, err
			}
			return panel.AsUIElement()
		}},
		{"A button, smaller than the viewport", func() (*uixaml.IUIElement, error) {
			control, err := uixaml.NewButton()
			if err != nil {
				return nil, err
			}
			if err := app.SetContent(control.AsContentControl, "Content that fits"); err != nil {
				return nil, err
			}
			return control.AsUIElement()
		}},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, content := range contents {
		caption, err := label(content.caption)
		if err != nil {
			return nil, err
		}
		view, err := newScrollView(320, 200, content.build)
		if err != nil {
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

// ScrollViewDynamicPage is the source's API exerciser — 594 lines of markup driving
// every method and property. The ones that change the view are here, with the readout
// the source keeps beside them.
//
// ScrollTo and ScrollBy return an int32 correlation id, not void. The id matches the
// ScrollCompleted event's, which is how a caller tells which of several overlapping
// view changes finished. It is reported here for exactly that reason.
func buildScrollViewDynamicPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(900, 1600, 8)
	if err != nil {
		return nil, err
	}
	view, err := newScrollView(420, 280, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		view.SetZoomMode(uixaml.ScrollingZoomModeEnabled),
		view.SetMinZoomFactor(0.5),
		view.SetMaxZoomFactor(3),
		view.SetContentOrientation(uixaml.ScrollingContentOrientationBoth),
	); err != nil {
		return nil, err
	}

	readout, err := newViewReadout()
	if err != nil {
		return nil, err
	}
	refresh := func() {
		horizontal, err := view.HorizontalOffset()
		if err != nil {
			readout.fail(err)
			return
		}
		vertical, err := view.VerticalOffset()
		if err != nil {
			readout.fail(err)
			return
		}
		zoom, err := view.ZoomFactor()
		if err != nil {
			readout.fail(err)
			return
		}
		readout.set(horizontal, vertical, zoom)
	}

	// ViewChanged fires for every offset or zoom change, however caused — a wheel, a
	// touch manipulation, or one of the buttons below. Reading the offsets from the
	// event is the only way to track an interactive change; polling after a call misses
	// everything the user does.
	if _, err := app.On(view.AddViewChanged, uixaml.NewTypedEventHandlerOfScrollViewAndObject,
		func(_ *uixaml.IScrollView, _ *syswinrt.IInspectable) { refresh() }); err != nil {
		return nil, err
	}

	correlation, err := label("No view change requested yet.")
	if err != nil {
		return nil, err
	}
	report := func(name string, id int32, err error) {
		if err != nil {
			_ = correlation.SetText(name + " failed: " + err.Error())
			return
		}
		_ = correlation.SetText(fmt.Sprintf("%s → correlation id %d", name, id))
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("ScrollTo 0,400", func() {
				id, err := view.ScrollTo(0, 400)
				report("ScrollTo", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("ScrollBy +100", func() {
				id, err := view.ScrollBy(0, 100)
				report("ScrollBy", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("Animated ScrollTo 0,0", func() {
				options, err := uixaml.NewScrollingScrollOptions(uixaml.ScrollingAnimationModeEnabled)
				if err != nil {
					report("ScrollTo", 0, err)
					return
				}
				defer options.Release()
				scrollOptions, err := options.AsScrollingScrollOptions()
				if err != nil {
					report("ScrollTo", 0, err)
					return
				}
				defer scrollOptions.Release()
				id, err := view.ScrollToWithOptions(0, 0, scrollOptions)
				report("Animated ScrollTo", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("ZoomBy ×1.25", func() {
				// A null centre point zooms about the viewport centre, which is what
				// the source's default is. ScrollPresenterDynamicPage shows the other
				// case, where a point has to be boxed into an IReference.
				id, err := view.ZoomBy(1.25, nil)
				report("ZoomBy", id, err)
			})
		},
	)
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, readout.element, correlation.AsUIElement, row.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ScrollViewWithRTLFlowDirectionPage: the same view in both flow directions. Under
// right-to-left, horizontal offset 0 is the RIGHT edge, which is the thing the source
// page exists to make visible.
func buildScrollViewWithRTLFlowDirectionPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, direction := range []uixaml.FlowDirection{
		uixaml.FlowDirectionLeftToRight, uixaml.FlowDirectionRightToLeft,
	} {
		caption, err := label(direction.String())
		if err != nil {
			return nil, err
		}
		content, err := bigContent(900, 300, 5)
		if err != nil {
			return nil, err
		}
		view, err := newScrollView(380, 220, content.AsUIElement)
		if err != nil {
			return nil, err
		}
		if err := app.All(
			view.SetContentOrientation(uixaml.ScrollingContentOrientationHorizontal),
			app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return frame.SetFlowDirection(direction)
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

// ScrollViewKeyboardAndGamepadNavigationPage: focusable content inside a view, with the
// navigation properties the source varies.
//
// Tab moves focus and the view follows it; the gamepad's XY focus does the same through
// XYFocusKeyboardNavigation. Both are UIElement properties rather than ScrollView ones,
// which is the point — the view reacts to focus moving, it does not implement the moving.
func buildScrollViewKeyboardAndGamepadNavigationPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	inner, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(inner.SetOrientation(uixaml.OrientationVertical), inner.SetSpacing(180)); err != nil {
		return nil, err
	}
	for i := 1; i <= 8; i++ {
		control, err := uixaml.NewButton()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			app.SetContent(control.AsContentControl, fmt.Sprintf("Focusable %d", i)),
			app.Append(inner.AsPanel, control.AsUIElement),
		); err != nil {
			return nil, err
		}
	}

	view, err := newScrollView(400, 260, inner.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.All(
			element.SetXYFocusKeyboardNavigation(uixaml.XYFocusKeyboardNavigationModeEnabled),
			element.SetTabFocusNavigation(uixaml.KeyboardNavigationModeLocal),
		)
	}); err != nil {
		return nil, err
	}

	note, err := label("Tab between the buttons: the view scrolls to follow focus.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollViewBringIntoViewPage: StartBringIntoView on an element deep inside the content.
//
// The request travels up the tree as a BringIntoViewRequested routed event, and the
// ScrollView answers it. Nothing addresses the view directly, which is why this works
// through any depth of nesting.
func buildScrollViewBringIntoViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	inner, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := inner.SetOrientation(uixaml.OrientationVertical); err != nil {
		return nil, err
	}
	var targets []*uixaml.Grid
	for i := 0; i < 10; i++ {
		band, err := colouredBand(560, 160, fmt.Sprintf("Target %d", i+1), bandColours[i%len(bandColours)])
		if err != nil {
			return nil, err
		}
		if err := app.Append(inner.AsPanel, band.AsUIElement); err != nil {
			return nil, err
		}
		targets = append(targets, band)
	}

	view, err := newScrollView(420, 260, inner.AsUIElement)
	if err != nil {
		return nil, err
	}

	bring := func(index int) func() (*uixaml.Button, error) {
		return func() (*uixaml.Button, error) {
			return button(fmt.Sprintf("Bring %d into view", index+1), func() {
				_ = app.With(targets[index].AsUIElement, func(element *uixaml.IUIElement) error {
					return element.StartBringIntoView()
				})
			})
		}
	}
	row, err := buttonRow(bring(0), bring(4), bring(9))
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, row.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollViewWithScrollControllersPage: the source replaces the template's scroll bars
// with its own IScrollController implementations.
//
// Those are C# classes in the test app, and the Go equivalent is
// ScrollPresenterWithSimpleScrollControllersPage in this batch, which implements the
// interface directly. What this page shows is the part that needs no implementation: the
// visibility properties that decide whether the built-in controllers appear at all,
// including the computed values, which are what a controller actually reacts to.
func buildScrollViewWithScrollControllersPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	visibilities := []uixaml.ScrollingScrollBarVisibility{
		uixaml.ScrollingScrollBarVisibilityAuto,
		uixaml.ScrollingScrollBarVisibilityVisible,
		uixaml.ScrollingScrollBarVisibilityHidden,
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, visibility := range visibilities {
		content, err := bigContent(700, 900, 5)
		if err != nil {
			return nil, err
		}
		view, err := newScrollView(320, 180, content.AsUIElement)
		if err != nil {
			return nil, err
		}
		if err := app.All(
			view.SetVerticalScrollBarVisibility(visibility),
			view.SetHorizontalScrollBarVisibility(visibility),
			view.SetContentOrientation(uixaml.ScrollingContentOrientationBoth),
		); err != nil {
			return nil, err
		}

		computedVertical, err := view.ComputedVerticalScrollBarVisibility()
		if err != nil {
			return nil, err
		}
		caption, err := label(fmt.Sprintf("%v → computed vertical %v", visibility, computedVertical))
		if err != nil {
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
