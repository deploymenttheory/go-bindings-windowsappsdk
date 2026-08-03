//go:build windows && amd64

package pages

// ParallaxView and PullToRefresh.
//
// Sources: controls/dev/{ParallaxView,PullToRefresh}/TestUI/*Page.xaml
//
// Both are controls that react to someone else's scrolling. ParallaxView watches a
// scroller and moves its child by a fraction of the distance; RefreshContainer watches
// one for an over-pull past the top. Neither scrolls anything itself, which is why every
// page here is really two controls wired together.

import (
	"fmt"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "ParallaxView", Name: "ParallaxViewPage", Build: buildParallaxViewPage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "SimpleRectanglePage", Build: buildParallaxSimpleRectanglePage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "TextPage", Build: buildParallaxTextPage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "DynamicPage", Build: buildParallaxDynamicPage})
	register(Page{Control: "ParallaxView", Name: "ParallaxViewStackPanelPage", Build: buildParallaxStackPanelPage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "VirtualizingStackPanelPage", Build: buildParallaxVirtualizingStackPanelPage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "ListViewBackgroundPage", Build: buildParallaxListViewBackgroundPage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "ListViewHeaderPage", Build: buildParallaxListViewHeaderPage, Inert: parallaxReason})
	register(Page{Control: "ParallaxView", Name: "ListViewItemPage", Build: buildParallaxListViewItemPage, Inert: parallaxReason})

	register(Page{Control: "PullToRefresh", Name: "PullToRefreshPage", Build: buildPullToRefreshPage})
	register(Page{Control: "PullToRefresh", Name: "PTRPage", Build: buildPTRPage})
	register(Page{Control: "PullToRefresh", Name: "RefreshContainerPage", Build: buildRefreshContainerPage})
	register(Page{Control: "PullToRefresh", Name: "RefreshContainerOnImagePage", Build: buildRefreshContainerOnImagePage})
	register(Page{Control: "PullToRefresh", Name: "RefreshVisualizerPage", Build: buildRefreshVisualizerPage})
	register(Page{Control: "PullToRefresh", Name: "ScrollViewerAdapterPage", Build: buildScrollViewerAdapterPage})
}

// parallaxOver builds a ParallaxView whose source is the given scroller, with child as
// the thing that moves.
//
// Source is the property that makes this work: the ParallaxView does not contain the
// scroller and is not contained by it — it merely watches it, which is what lets the
// moving layer sit BEHIND the scrolling content rather than inside it.
func parallaxOver(source *uixaml.IUIElement, child func() (*uixaml.IUIElement, error),
	verticalShift float64,
) (*uixaml.ParallaxView, error) {
	view, err := uixaml.NewParallaxView()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		view.SetSource(source),
		view.SetVerticalShift(verticalShift),
		view.SetVerticalSourceOffsetKind(uixaml.ParallaxSourceOffsetKindAbsolute),
		app.With(child, view.SetChild),
	); err != nil {
		return nil, err
	}
	return view, nil
}

// scrollerOverParallax is the arrangement every ParallaxView page uses: a Grid with the
// parallax layer beneath and the scrolling content on top of it.
func scrollerOverParallax(child func() (*uixaml.IUIElement, error), shift float64,
	content func() (*uixaml.IUIElement, error), width, height float64,
) (*uixaml.Grid, error) {
	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	if err := app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(width), frame.SetHeight(height))
	}); err != nil {
		return nil, err
	}

	scroller, err := newScrollView(width, height, content)
	if err != nil {
		return nil, err
	}
	scrollerElement, err := scroller.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer scrollerElement.Release()

	view, err := parallaxOver(scrollerElement, child, shift)
	if err != nil {
		return nil, err
	}
	// The parallax layer is added FIRST so it sits behind: a Grid stacks its children in
	// document order, and the moving layer is the background.
	if err := app.Append(grid.AsPanel, view.AsUIElement, scroller.AsUIElement); err != nil {
		return nil, err
	}
	return grid, nil
}

// tallContent is the scrolling content these pages parallax against.
func tallContent() (*uixaml.IUIElement, error) {
	panel, err := bigContent(420, 1600, 8)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildParallaxViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	grid, err := scrollerOverParallax(func() (*uixaml.IUIElement, error) {
		band, err := colouredBand(440, 700, "Parallax layer", bandColours[2])
		if err != nil {
			return nil, err
		}
		return band.AsUIElement()
	}, 200, tallContent, 440, 320)
	if err != nil {
		return nil, err
	}
	caption, err := label("Scroll the content: the layer behind it moves by a fraction of " +
		"the distance.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, grid.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// SimpleRectanglePage: the smallest possible parallax child.
func buildParallaxSimpleRectanglePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	grid, err := scrollerOverParallax(func() (*uixaml.IUIElement, error) {
		band, err := colouredBand(440, 600, "", bandColours[0])
		if err != nil {
			return nil, err
		}
		return band.AsUIElement()
	}, 150, tallContent, 440, 300)
	if err != nil {
		return nil, err
	}
	return grid.AsUIElement()
}

// TextPage: text as the parallax child, which the source uses because text makes the
// sub-pixel movement obvious.
func buildParallaxTextPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	grid, err := scrollerOverParallax(func() (*uixaml.IUIElement, error) {
		block, err := label("PARALLAX")
		if err != nil {
			return nil, err
		}
		if err := app.All(
			block.SetFontSize(72),
			app.With(block.AsUIElement, func(element *uixaml.IUIElement) error {
				return element.SetOpacity(0.35)
			}),
		); err != nil {
			return nil, err
		}
		return block.AsUIElement()
	}, 120, tallContent, 440, 300)
	if err != nil {
		return nil, err
	}
	return grid.AsUIElement()
}

// DynamicPage: the shift changed while the page is live, which is what the source's
// sliders do.
func buildParallaxDynamicPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(420, 1600, 8)
	if err != nil {
		return nil, err
	}
	scroller, err := newScrollView(420, 260, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	scrollerElement, err := scroller.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer scrollerElement.Release()

	view, err := parallaxOver(scrollerElement, func() (*uixaml.IUIElement, error) {
		band, err := colouredBand(420, 700, "Moving layer", bandColours[3])
		if err != nil {
			return nil, err
		}
		return band.AsUIElement()
	}, 100)
	if err != nil {
		return nil, err
	}

	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(420), frame.SetHeight(260))
		}),
		app.Append(grid.AsPanel, view.AsUIElement, scroller.AsUIElement),
	); err != nil {
		return nil, err
	}

	status, err := label("VerticalShift: 100")
	if err != nil {
		return nil, err
	}
	shift := 100.0
	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Shift +100", func() {
				shift += 100
				if err := view.SetVerticalShift(shift); err != nil {
					_ = status.SetText("Setting the shift failed: " + err.Error())
					return
				}
				_ = status.SetText(fmt.Sprintf("VerticalShift: %.0f", shift))
			})
		},
		func() (*uixaml.Button, error) {
			return button("Shift 0", func() {
				shift = 0
				_ = view.SetVerticalShift(shift)
				_ = status.SetText("VerticalShift: 0 — the layer no longer moves.")
			})
		},
	)
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, row.AsUIElement, grid.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ParallaxViewStackPanelPage and VirtualizingStackPanelPage: the source scroller is a
// panel inside a ScrollViewer rather than a ScrollView.
//
// ParallaxView takes any UIElement as its Source and finds the scroller from it, which is
// the point of both pages — the parallax does not care what is doing the scrolling.
func buildParallaxStackPanelPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return parallaxOverViewer(false)
}

func buildParallaxVirtualizingStackPanelPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return parallaxOverViewer(true)
}

// parallaxOverViewer builds the ScrollViewer variant, optionally with a virtualizing
// items control inside it.
func parallaxOverViewer(virtualizing bool) (*uixaml.IUIElement, error) {
	viewer, err := uixaml.NewScrollViewer()
	if err != nil {
		return nil, err
	}
	if err := app.With(viewer.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(420), frame.SetHeight(260))
	}); err != nil {
		return nil, err
	}

	var inner *uixaml.IUIElement
	if virtualizing {
		list, err := uixaml.NewListView()
		if err != nil {
			return nil, err
		}
		if err := fillItems(unsafe.Pointer(list), numbered("Virtualized row", 60)); err != nil {
			return nil, err
		}
		inner, err = list.AsUIElement()
		if err != nil {
			return nil, err
		}
	} else {
		panel, err := bigContent(400, 1400, 7)
		if err != nil {
			return nil, err
		}
		inner, err = panel.AsUIElement()
		if err != nil {
			return nil, err
		}
	}
	defer inner.Release()
	if err := app.With(viewer.AsContentControl, func(control *uixaml.IContentControl) error {
		return control.SetContent(inspectableOf(inner))
	}); err != nil {
		return nil, err
	}

	viewerElement, err := viewer.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer viewerElement.Release()

	view, err := parallaxOver(viewerElement, func() (*uixaml.IUIElement, error) {
		band, err := colouredBand(420, 600, "Behind a ScrollViewer", bandColours[1])
		if err != nil {
			return nil, err
		}
		return band.AsUIElement()
	}, 160)
	if err != nil {
		return nil, err
	}

	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(420), frame.SetHeight(260))
		}),
		app.Append(grid.AsPanel, view.AsUIElement, viewer.AsUIElement),
	); err != nil {
		return nil, err
	}
	return grid.AsUIElement()
}

// The three ListView* pages put the parallax in a different place each time: behind the
// whole list, in its header, and inside each item.
func buildParallaxListViewBackgroundPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	list, err := uixaml.NewListView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(list), numbered("Row", 40)); err != nil {
		return nil, err
	}
	if err := app.With(list.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(420), frame.SetHeight(300))
	}); err != nil {
		return nil, err
	}

	listElement, err := list.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer listElement.Release()
	view, err := parallaxOver(listElement, func() (*uixaml.IUIElement, error) {
		band, err := colouredBand(420, 700, "Behind the list", bandColours[4])
		if err != nil {
			return nil, err
		}
		return band.AsUIElement()
	}, 180)
	if err != nil {
		return nil, err
	}

	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(420), frame.SetHeight(300))
		}),
		app.Append(grid.AsPanel, view.AsUIElement, list.AsUIElement),
	); err != nil {
		return nil, err
	}
	return grid.AsUIElement()
}

func buildParallaxListViewHeaderPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	list, err := uixaml.NewListView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(list), numbered("Row", 40)); err != nil {
		return nil, err
	}
	if err := app.With(list.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(420), frame.SetHeight(320))
	}); err != nil {
		return nil, err
	}

	listElement, err := list.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer listElement.Release()
	view, err := parallaxOver(listElement, func() (*uixaml.IUIElement, error) {
		band, err := colouredBand(420, 260, "Parallax header", bandColours[0])
		if err != nil {
			return nil, err
		}
		return band.AsUIElement()
	}, 80)
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(140)
	}); err != nil {
		return nil, err
	}

	// The header is set on the ListViewBase, which is where Header lives.
	if err := app.With(list.AsListViewBase, func(base *uixaml.IListViewBase) error {
		return app.With(view.AsUIElement, func(element *uixaml.IUIElement) error {
			return base.SetHeader(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}
	return list.AsUIElement()
}

func buildParallaxListViewItemPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	note, err := label("The source puts a ParallaxView inside each item's template, so " +
		"every row parallaxes against the list it is in.\n\n" +
		"A ParallaxView needs its Source set to the scroller, and a DataTemplate cannot " +
		"reach outside itself to name one — in XAML that is an ElementName binding, " +
		"which resolves through the namescope the parser built. So the rows below are " +
		"built in Go, each with its Source wired to the list explicitly, which is what " +
		"the binding would have done.")
	if err != nil {
		return nil, err
	}

	list, err := uixaml.NewListView()
	if err != nil {
		return nil, err
	}
	if err := app.With(list.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(440), frame.SetHeight(320))
	}); err != nil {
		return nil, err
	}
	listElement, err := list.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer listElement.Release()

	// The rows go straight into Items, since each is already an element.
	itemsControl, err := list.AsItemsControl()
	if err != nil {
		return nil, err
	}
	defer itemsControl.Release()
	observable, err := itemsControl.Items()
	if err != nil {
		return nil, err
	}
	defer observable.Release()
	items, err := observable.AsVectorOfObject()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	for i := 0; i < 12; i++ {
		row, err := parallaxOver(listElement, func() (*uixaml.IUIElement, error) {
			band, err := colouredBand(400, 160, fmt.Sprintf("Row %d", i+1),
				bandColours[i%len(bandColours)])
			if err != nil {
				return nil, err
			}
			return band.AsUIElement()
		}, 40)
		if err != nil {
			return nil, err
		}
		if err := app.With(row.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(80)
		}); err != nil {
			return nil, err
		}
		err = items.Append(inspectableOf(&row.IParallaxView))
		row.Release()
		if err != nil {
			return nil, err
		}
	}

	panel, err := stack(8, note.AsUIElement, list.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// newRefreshContainer builds a RefreshContainer round a list, with the handler that
// completes the refresh.
//
// A refresh is a DEFERRAL, not a callback that ends when it returns: the visualizer keeps
// spinning until the deferral is completed, which is what lets an application do real
// work. Completing it immediately, as here, is the shortest correct handler; forgetting
// to complete it leaves the spinner running forever, which is the bug this shape avoids.
func newRefreshContainer(direction uixaml.RefreshPullDirection, status *uixaml.TextBlock,
) (*uixaml.RefreshContainer, *uixaml.ListView, error) {
	container, err := uixaml.NewRefreshContainer()
	if err != nil {
		return nil, nil, err
	}
	if err := container.SetPullDirection(direction); err != nil {
		return nil, nil, err
	}

	list, err := uixaml.NewListView()
	if err != nil {
		return nil, nil, err
	}
	if err := fillItems(unsafe.Pointer(list), numbered("Item", 25)); err != nil {
		return nil, nil, err
	}
	if err := app.With(list.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(container.AsContentControl, func(control *uixaml.IContentControl) error {
			return control.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, nil, err
	}

	refreshes := 0
	if _, err := app.On(container.AddRefreshRequested,
		uixaml.NewTypedEventHandlerOfRefreshContainerAndRefreshRequestedEventArgs,
		func(_ *uixaml.IRefreshContainer, args *uixaml.IRefreshRequestedEventArgs) {
			refreshes++
			deferral, err := args.GetDeferral()
			if err != nil {
				_ = status.SetText("Taking the deferral failed: " + err.Error())
				return
			}
			defer deferral.Release()
			// Real work would go here, and Complete would follow it.
			if err := deferral.Complete(); err != nil {
				_ = status.SetText("Completing the deferral failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("%d refresh(es) requested and completed.", refreshes))
		}); err != nil {
		return nil, nil, err
	}

	if err := app.With(container.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(420), frame.SetHeight(300))
	}); err != nil {
		return nil, nil, err
	}
	return container, list, nil
}

func buildPullToRefreshPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Pull down past the top, or press the button.")
	if err != nil {
		return nil, err
	}
	container, _, err := newRefreshContainer(uixaml.RefreshPullDirectionTopToBottom, status)
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("RequestRefresh", func() {
			if err := container.RequestRefresh(); err != nil {
				_ = status.SetText("RequestRefresh failed: " + err.Error())
			}
		})
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, container.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// PTRPage is the source's short name for the same control; it exists to be navigated to
// from the index, and ports as the same arrangement with the other pull direction.
func buildPTRPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Pull UP from the bottom on this one.")
	if err != nil {
		return nil, err
	}
	container, _, err := newRefreshContainer(uixaml.RefreshPullDirectionBottomToTop, status)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, container.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// RefreshContainerPage: every pull direction, side by side.
func buildRefreshContainerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	directions := []struct {
		caption string
		value   uixaml.RefreshPullDirection
	}{
		{"TopToBottom", uixaml.RefreshPullDirectionTopToBottom},
		{"BottomToTop", uixaml.RefreshPullDirectionBottomToTop},
		{"LeftToRight", uixaml.RefreshPullDirectionLeftToRight},
		{"RightToLeft", uixaml.RefreshPullDirectionRightToLeft},
	}
	status, err := label("Each container pulls from a different edge.")
	if err != nil {
		return nil, err
	}

	children := []func() (*uixaml.IUIElement, error){status.AsUIElement}
	for _, direction := range directions {
		caption, err := label("PullDirection " + direction.caption)
		if err != nil {
			return nil, err
		}
		container, _, err := newRefreshContainer(direction.value, status)
		if err != nil {
			return nil, err
		}
		if err := app.With(container.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(180)
		}); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, container.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// RefreshContainerOnImagePage: the container round something that is not a list, which
// is the case that shows the container does not require a scroller at all.
func buildRefreshContainerOnImagePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	container, err := uixaml.NewRefreshContainer()
	if err != nil {
		return nil, err
	}
	band, err := colouredBand(420, 280, "Not a list", bandColours[2])
	if err != nil {
		return nil, err
	}
	if err := app.With(band.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(container.AsContentControl, func(control *uixaml.IContentControl) error {
			return control.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}

	status, err := label("A RefreshContainer round a plain element.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(container.AddRefreshRequested,
		uixaml.NewTypedEventHandlerOfRefreshContainerAndRefreshRequestedEventArgs,
		func(_ *uixaml.IRefreshContainer, args *uixaml.IRefreshRequestedEventArgs) {
			deferral, err := args.GetDeferral()
			if err != nil {
				return
			}
			defer deferral.Release()
			_ = deferral.Complete()
			_ = status.SetText("Refreshed.")
		}); err != nil {
		return nil, err
	}

	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("RequestRefresh", func() { _ = container.RequestRefresh() })
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, container.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// RefreshVisualizerPage: the spinner on its own, without a container.
//
// The visualizer is what the container shows; it has its own RequestRefresh and its own
// State, and separating them is what lets an application put the spinner somewhere other
// than at the pulled edge.
func buildRefreshVisualizerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	visualizer, err := uixaml.NewRefreshVisualizer()
	if err != nil {
		return nil, err
	}
	if err := app.With(visualizer.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(120), frame.SetHeight(120))
	}); err != nil {
		return nil, err
	}

	status, err := label("State: Idle")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(visualizer.AddRefreshStateChanged,
		uixaml.NewTypedEventHandlerOfRefreshVisualizerAndRefreshStateChangedEventArgs,
		func(sender *uixaml.IRefreshVisualizer, _ *uixaml.IRefreshStateChangedEventArgs) {
			state, err := sender.State()
			if err != nil {
				return
			}
			_ = status.SetText("State: " + state.String())
		}); err != nil {
		return nil, err
	}
	if _, err := app.On(visualizer.AddRefreshRequested,
		uixaml.NewTypedEventHandlerOfRefreshVisualizerAndRefreshRequestedEventArgs,
		func(_ *uixaml.IRefreshVisualizer, args *uixaml.IRefreshRequestedEventArgs) {
			deferral, err := args.GetDeferral()
			if err != nil {
				return
			}
			defer deferral.Release()
			_ = deferral.Complete()
		}); err != nil {
		return nil, err
	}

	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("RequestRefresh on the visualizer", func() {
			if err := visualizer.RequestRefresh(); err != nil {
				_ = status.SetText("RequestRefresh failed: " + err.Error())
			}
		})
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, visualizer.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollViewerAdapterPage: the source wires a RefreshContainer to a ScrollViewer through
// ScrollViewerIRefreshInfoProviderAdapter.
//
// That adapter is test-app code in the source, not a shipped type — the container finds a
// ScrollViewer inside itself without help, which is what this shows: a ScrollViewer as
// the container's content, pulled from the top.
func buildScrollViewerAdapterPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	container, err := uixaml.NewRefreshContainer()
	if err != nil {
		return nil, err
	}
	content, err := bigContent(400, 1200, 6)
	if err != nil {
		return nil, err
	}
	viewer, err := uixaml.NewScrollViewer()
	if err != nil {
		return nil, err
	}
	if err := app.With(content.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(viewer.AsContentControl, func(control *uixaml.IContentControl) error {
			return control.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}
	if err := app.With(viewer.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(container.AsContentControl, func(control *uixaml.IContentControl) error {
			return control.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}
	if err := app.With(container.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(420), frame.SetHeight(300))
	}); err != nil {
		return nil, err
	}

	status, err := label("A ScrollViewer inside a RefreshContainer: no adapter needed.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(container.AddRefreshRequested,
		uixaml.NewTypedEventHandlerOfRefreshContainerAndRefreshRequestedEventArgs,
		func(_ *uixaml.IRefreshContainer, args *uixaml.IRefreshRequestedEventArgs) {
			deferral, err := args.GetDeferral()
			if err != nil {
				return
			}
			defer deferral.Release()
			_ = deferral.Complete()
			_ = status.SetText("Refreshed from the ScrollViewer's pull.")
		}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, container.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}
