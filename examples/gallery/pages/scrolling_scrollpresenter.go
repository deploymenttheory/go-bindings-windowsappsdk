//go:build windows && amd64

package pages

// The ScrollPresenter pages.
//
// Sources: controls/dev/ScrollPresenter/TestUI/*Page.xaml
//
// ScrollPresenter is the primitive underneath ScrollView: it does the scrolling and the
// zooming and has no chrome, no scroll bars and no template. ScrollView is a Control
// wrapping one. The sources test the primitive separately because a bug in ScrollView is
// usually a bug here.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtnumerics "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation/numerics"
)

func init() {
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterPage", Build: buildScrollPresenterPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresentersWithSimpleContentsPage", Build: buildScrollPresentersWithSimpleContentsPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterDynamicPage", Build: buildScrollPresenterDynamicPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterExpressionAnimationSourcesPage", Build: buildScrollPresenterExpressionAnimationSourcesPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterChainingAndRailingPage", Build: buildScrollPresenterChainingAndRailingPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterStackPanelAnchoringPage", Build: buildScrollPresenterStackPanelAnchoringPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterRepeaterAnchoringPage", Build: buildScrollPresenterRepeaterAnchoringPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterScrollSnapPointsPage", Build: buildScrollPresenterScrollSnapPointsPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterZoomSnapPointsPage", Build: buildScrollPresenterZoomSnapPointsPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterBringIntoViewPage", Build: buildScrollPresenterBringIntoViewPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterManipulationModePage", Build: buildScrollPresenterManipulationModePage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterAccessibilityPage", Build: buildScrollPresenterAccessibilityPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterMousePanningPage", Build: buildScrollPresenterMousePanningPage})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterLeakDetectionPage", Build: buildScrollPresenterLeakDetectionPage})
}

// newScrollPresenter is the equivalent of newScrollView for the primitive.
func newScrollPresenter(width, height float64, content func() (*uixaml.IUIElement, error)) (*uixaml.ScrollPresenter, error) {
	presenter, err := uixaml.NewScrollPresenter()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(presenter.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(width), frame.SetHeight(height))
		}),
		app.With(content, presenter.SetContent),
	); err != nil {
		return nil, err
	}
	return presenter, nil
}

func buildScrollPresenterPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return navigationIndex("ScrollPresenter", []string{
		"ScrollPresentersWithSimpleContentsPage — one presenter per kind of content",
		"ScrollPresenterDynamicPage — exercises the ScrollPresenter API",
		"ScrollPresenterExpressionAnimationSourcesPage — the animatable property set",
		"ScrollPresenterChainingAndRailingPage — chaining and railing modes",
		"ScrollPresenterStackPanelAnchoringPage — anchoring within a StackPanel",
		"ScrollPresenterRepeaterAnchoringPage — anchoring within an ItemsRepeater",
		"ScrollPresenterScrollSnapPointsPage — single and repeated scroll snap points",
		"ScrollPresenterZoomSnapPointsPage — single and repeated zoom snap points",
		"ScrollPresenterBringIntoViewPage — StartBringIntoView and BringingIntoView",
		"ScrollPresenterManipulationModePage — ManipulationMode and IgnoredInputKinds",
		"ScrollPresenterAccessibilityPage — the automation peer",
		"ScrollPresenterMousePanningPage — panning with a mouse",
		"ScrollPresenterLeakDetectionPage — create and drop presenters",
		"ScrollPresenterWithSimpleScrollControllersPage — a Go IScrollController",
		"ScrollPresenterWithCompositionScrollControllersPage — composition-driven",
		"ScrollPresenterWithBiDirectionalScrollControllerPage — one controller, both axes",
	})
}

func buildScrollPresentersWithSimpleContentsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	orientations := []struct {
		caption string
		value   uixaml.ScrollingContentOrientation
	}{
		{"Vertical", uixaml.ScrollingContentOrientationVertical},
		{"Horizontal", uixaml.ScrollingContentOrientationHorizontal},
		{"Both", uixaml.ScrollingContentOrientationBoth},
		{"None", uixaml.ScrollingContentOrientationNone},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, orientation := range orientations {
		caption, err := label("ContentOrientation " + orientation.caption)
		if err != nil {
			return nil, err
		}
		content, err := bigContent(700, 900, 5)
		if err != nil {
			return nil, err
		}
		presenter, err := newScrollPresenter(300, 180, content.AsUIElement)
		if err != nil {
			return nil, err
		}
		if err := presenter.SetContentOrientation(orientation.value); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, presenter.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ScrollPresenterDynamicPage is the source's largest page in this family, and the one
// that drives every view-changing method.
//
// It is also where IReference<Vector2> is unavoidable. ZoomTo takes a nullable centre
// point, and passing a real one needs an object implementing IReference<Vector2> —
// which Windows.Foundation.PropertyValue cannot produce for a numerics type. See
// app/reference.go; app.NewReference is what makes this page's zoom-about-a-point work.
func buildScrollPresenterDynamicPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(1000, 1600, 8)
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(440, 300, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		presenter.SetContentOrientation(uixaml.ScrollingContentOrientationBoth),
		presenter.SetZoomMode(uixaml.ScrollingZoomModeEnabled),
		presenter.SetMinZoomFactor(0.25),
		presenter.SetMaxZoomFactor(4),
	); err != nil {
		return nil, err
	}

	readout, err := newViewReadout()
	if err != nil {
		return nil, err
	}
	refresh := func() {
		horizontal, err := presenter.HorizontalOffset()
		if err != nil {
			readout.fail(err)
			return
		}
		vertical, err := presenter.VerticalOffset()
		if err != nil {
			readout.fail(err)
			return
		}
		zoom, err := presenter.ZoomFactor()
		if err != nil {
			readout.fail(err)
			return
		}
		readout.set(horizontal, vertical, zoom)
	}
	if _, err := app.On(presenter.AddViewChanged, uixaml.NewTypedEventHandlerOfScrollPresenterAndObject,
		func(_ *uixaml.IScrollPresenter, _ *syswinrt.IInspectable) { refresh() }); err != nil {
		return nil, err
	}

	status, err := label("No view change requested yet.")
	if err != nil {
		return nil, err
	}
	report := func(name string, id int32, err error) {
		if err != nil {
			_ = status.SetText(name + " failed: " + err.Error())
			return
		}
		_ = status.SetText(fmt.Sprintf("%s → correlation id %d", name, id))
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("ScrollTo 200,300", func() {
				id, err := presenter.ScrollTo(200, 300)
				report("ScrollTo", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("ScrollBy 0,+150", func() {
				id, err := presenter.ScrollBy(0, 150)
				report("ScrollBy", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("AddScrollVelocity", func() {
				// The inertia decay rate is the other IReference<Vector2> on this
				// interface. A null one means the platform default.
				id, err := presenter.AddScrollVelocity(
					wrtnumerics.Vector2{X: 0, Y: 600}, nil)
				report("AddScrollVelocity", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("ZoomTo ×2 about (100,100)", func() {
				centre, err := app.NewReference[uixaml.IReferenceOfVector2](
					wrtnumerics.Vector2{X: 100, Y: 100},
					&uixaml.IID_IReferenceOfVector2,
					"Windows.Foundation.IReference`1<Windows.Foundation.Numerics.Vector2>")
				if err != nil {
					report("ZoomTo", 0, err)
					return
				}
				defer centre.Close()
				id, err := presenter.ZoomTo(2, centre.Value())
				report("ZoomTo about a point", id, err)
			})
		},
		func() (*uixaml.Button, error) {
			return button("ZoomTo ×1 (centred)", func() {
				id, err := presenter.ZoomTo(1, nil)
				report("ZoomTo", id, err)
			})
		},
	)
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, readout.element, status.AsUIElement, row.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ScrollPresenterExpressionAnimationSourcesPage reads the CompositionPropertySet the
// presenter publishes.
//
// This is the seam between XAML and the compositor: the set carries Offset, Position,
// MinPosition, MaxPosition, ZoomFactor and Extent as composition properties, so an
// expression animation can be driven by the scroll position without any per-frame code.
// The page reports the values, which is what the source does before animating from them.
func buildScrollPresenterExpressionAnimationSourcesPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(800, 1200, 6)
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(400, 240, content.AsUIElement)
	if err != nil {
		return nil, err
	}

	sources, err := presenter.ExpressionAnimationSources()
	if err != nil {
		return nil, err
	}
	defer sources.Release()

	// Position is Vector2 and ZoomFactor is Scalar. TryGetVector2 and TryGetScalar both
	// report whether the property was there, so a missing one is not a zero.
	status, err := label("Reading the property set…")
	if err != nil {
		return nil, err
	}
	refresh := func() {
		// TryGet* writes through an out parameter and RETURNS the status, which is
		// the CompositionGetValueStatus — Succeeded, TypeMismatch or NotFound. The
		// error is the HRESULT of the call itself, so both have to be checked.
		var position wrtnumerics.Vector2
		positionStatus, err := sources.TryGetVector2("Position", &position)
		if err != nil {
			_ = status.SetText("TryGetVector2 failed: " + err.Error())
			return
		}
		var zoom float32
		zoomStatus, err := sources.TryGetScalar("ZoomFactor", &zoom)
		if err != nil {
			_ = status.SetText("TryGetScalar failed: " + err.Error())
			return
		}
		_ = status.SetText(fmt.Sprintf("Position %v (%v) — ZoomFactor %.2f (%v)",
			position, positionStatus, zoom, zoomStatus))
	}
	refresh()
	if _, err := app.On(presenter.AddViewChanged, uixaml.NewTypedEventHandlerOfScrollPresenterAndObject,
		func(_ *uixaml.IScrollPresenter, _ *syswinrt.IInspectable) { refresh() }); err != nil {
		return nil, err
	}

	note, err := label("Scroll the presenter: the values below come from the compositor's " +
		"property set, not from the XAML properties.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, status.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollPresenterChainingAndRailingPage: nested presenters, which is the only way
// chaining means anything.
//
// Chaining decides what happens when the inner one reaches its edge: Auto passes the
// remaining delta to the outer, Never stops there. Railing decides whether a
// mostly-vertical gesture is allowed to drift horizontally.
func buildScrollPresenterChainingAndRailingPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	modes := []struct {
		caption string
		chain   uixaml.ScrollingChainMode
		rail    uixaml.ScrollingRailMode
	}{
		{"Chaining Auto, railing Enabled", uixaml.ScrollingChainModeAuto, uixaml.ScrollingRailModeEnabled},
		{"Chaining Always, railing Disabled", uixaml.ScrollingChainModeAlways, uixaml.ScrollingRailModeDisabled},
		{"Chaining Never, railing Enabled", uixaml.ScrollingChainModeNever, uixaml.ScrollingRailModeEnabled},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range modes {
		caption, err := label(mode.caption)
		if err != nil {
			return nil, err
		}

		innerContent, err := bigContent(600, 900, 5)
		if err != nil {
			return nil, err
		}
		inner, err := newScrollPresenter(280, 140, innerContent.AsUIElement)
		if err != nil {
			return nil, err
		}
		if err := app.All(
			inner.SetVerticalScrollChainMode(mode.chain),
			inner.SetHorizontalScrollChainMode(mode.chain),
			inner.SetVerticalScrollRailMode(mode.rail),
			inner.SetHorizontalScrollRailMode(mode.rail),
			inner.SetContentOrientation(uixaml.ScrollingContentOrientationBoth),
		); err != nil {
			return nil, err
		}

		// The outer presenter needs content taller than itself as well, or there is
		// nothing for a chained delta to move.
		outerContent, err := uixaml.NewStackPanel()
		if err != nil {
			return nil, err
		}
		if err := outerContent.SetOrientation(uixaml.OrientationVertical); err != nil {
			return nil, err
		}
		above, err := colouredBand(320, 120, "Outer, above", bandColours[3])
		if err != nil {
			return nil, err
		}
		below, err := colouredBand(320, 120, "Outer, below", bandColours[4])
		if err != nil {
			return nil, err
		}
		if err := app.Append(outerContent.AsPanel,
			above.AsUIElement, inner.AsUIElement, below.AsUIElement); err != nil {
			return nil, err
		}
		outer, err := newScrollPresenter(340, 240, outerContent.AsUIElement)
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, outer.AsUIElement)
	}
	panel, err := stack(12, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// anchoringPage is the shape both anchoring pages share: content, an anchor ratio, and a
// button that adds an element ABOVE the current view.
//
// Anchoring is what stops that insertion from shifting what you are looking at. Without
// it, prepending 200 pixels of content moves everything down by 200; with a
// VerticalAnchorRatio of 0 the presenter compensates and the visible element stays put.
func anchoringPage(makeContent func() (*uixaml.StackPanel, error), note string) (*uixaml.IUIElement, error) {
	content, err := makeContent()
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(420, 260, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		presenter.SetVerticalAnchorRatio(0),
		presenter.SetHorizontalAnchorRatio(0),
	); err != nil {
		return nil, err
	}

	added := 0
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Insert a band at the top", func() {
			added++
			band, err := colouredBand(560, 120, fmt.Sprintf("Inserted %d", added),
				bandColours[added%len(bandColours)])
			if err != nil {
				return
			}
			_ = app.With(band.AsUIElement, func(element *uixaml.IUIElement) error {
				return app.With(content.AsPanel, func(panel *uixaml.IPanel) error {
					children, err := panel.Children()
					if err != nil {
						return err
					}
					defer children.Release()
					return children.InsertAt(0, element)
				})
			})
		})
	})
	if err != nil {
		return nil, err
	}

	caption, err := label(note)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, row.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildScrollPresenterStackPanelAnchoringPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return anchoringPage(func() (*uixaml.StackPanel, error) {
		return bigContent(560, 1400, 8)
	}, "Scroll down, then insert. With VerticalAnchorRatio 0 the visible band stays put.")
}

// ScrollPresenterRepeaterAnchoringPage does the same inside an ItemsRepeater, where the
// elements are realized on demand rather than all present.
//
// The repeater is given a StackLayout and a plain items source; anchoring then has to
// work against elements that may not exist yet, which is the harder case and why the
// source tests it separately.
func buildScrollPresenterRepeaterAnchoringPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	repeater, err := uixaml.NewItemsRepeater()
	if err != nil {
		return nil, err
	}
	layout, err := uixaml.NewStackLayout()
	if err != nil {
		return nil, err
	}
	if err := layout.SetSpacing(4); err != nil {
		return nil, err
	}
	layoutBase, err := layout.AsLayout()
	if err != nil {
		return nil, err
	}
	defer layoutBase.Release()
	if err := repeater.SetLayout(layoutBase); err != nil {
		return nil, err
	}

	template, err := app.LoadMarkup[uixaml.IDataTemplate](
		app.Markup(`<DataTemplate><TextBlock Text="{Binding}" Height="40" Margin="4"/></DataTemplate>`),
		&uixaml.IID_IDataTemplate)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	// ItemTemplate is typed IInspectable rather than IElementFactory: it accepts
	// either a DataTemplate or an IElementFactory, and the control sorts out which.
	if err := repeater.SetItemTemplate((*syswinrt.IInspectable)(unsafe.Pointer(template))); err != nil {
		return nil, err
	}

	// ItemsRepeater has no Items property — ItemsSource is the only route, and it takes
	// a WinRT collection. See app/itemssource.go for why that needs this package's IIDs.
	items, err := app.NewStringItemsSource(numbered("Repeated item", 40), winrt.CollectionIIDs{
		Iterable:   uixaml.IID_IIterableOfObject,
		Iterator:   uixaml.IID_IIteratorOfObject,
		VectorView: uixaml.IID_IVectorViewOfObject,
		Vector:     uixaml.IID_IVectorOfObject,
	})
	if err != nil {
		return nil, err
	}
	if err := repeater.SetItemsSource(items.Inspectable()); err != nil {
		items.Close()
		return nil, err
	}

	presenter, err := newScrollPresenter(420, 260, repeater.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := presenter.SetVerticalAnchorRatio(0); err != nil {
		return nil, err
	}

	note, err := label("An ItemsRepeater inside a ScrollPresenter: elements are realized " +
		"as they come into view, and anchoring works against ones that may not exist yet.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollPresenterScrollSnapPointsPage: both kinds of scroll snap point.
//
// A ScrollSnapPoint is one position; a RepeatedScrollSnapPoint is an interval over a
// range, which is how you snap to every item without declaring one per item. The
// alignment says which edge of the viewport the point aligns to.
func buildScrollPresenterScrollSnapPointsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(560, 1600, 8)
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(420, 200, content.AsUIElement)
	if err != nil {
		return nil, err
	}

	vertical, err := presenter.VerticalSnapPoints()
	if err != nil {
		return nil, err
	}
	defer vertical.Release()

	// One explicit point, then a repeated one covering the rest of the extent.
	single, err := uixaml.NewScrollSnapPoint(0, uixaml.ScrollSnapPointsAlignmentNear)
	if err != nil {
		return nil, err
	}
	defer single.Release()
	singleBase, err := single.AsScrollSnapPointBase()
	if err != nil {
		return nil, err
	}
	defer singleBase.Release()
	if err := vertical.Append(singleBase); err != nil {
		return nil, err
	}

	repeated, err := uixaml.NewRepeatedScrollSnapPoint(200, 200, 200, 1600,
		uixaml.ScrollSnapPointsAlignmentNear)
	if err != nil {
		return nil, err
	}
	defer repeated.Release()
	repeatedBase, err := repeated.AsScrollSnapPointBase()
	if err != nil {
		return nil, err
	}
	defer repeatedBase.Release()
	if err := vertical.Append(repeatedBase); err != nil {
		return nil, err
	}

	count, err := vertical.Size()
	if err != nil {
		return nil, err
	}
	note, err := label(fmt.Sprintf("%d vertical snap points: one at 0, then every 200 "+
		"from 200 to 1600. Flick the content and it settles on one.", count))
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildScrollPresenterZoomSnapPointsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(800, 1000, 5)
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(420, 240, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		presenter.SetZoomMode(uixaml.ScrollingZoomModeEnabled),
		presenter.SetMinZoomFactor(0.5),
		presenter.SetMaxZoomFactor(4),
	); err != nil {
		return nil, err
	}

	points, err := presenter.ZoomSnapPoints()
	if err != nil {
		return nil, err
	}
	defer points.Release()
	for _, factor := range []float64{0.5, 1, 2, 4} {
		point, err := uixaml.NewZoomSnapPoint(factor)
		if err != nil {
			return nil, err
		}
		base, err := point.AsZoomSnapPointBase()
		point.Release()
		if err != nil {
			return nil, err
		}
		err = points.Append(base)
		base.Release()
		if err != nil {
			return nil, err
		}
	}

	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("ZoomTo ×1.7 — settles on ×2", func() {
			_, _ = presenter.ZoomTo(1.7, nil)
		})
	})
	if err != nil {
		return nil, err
	}
	note, err := label("Zoom snap points at 0.5, 1, 2 and 4.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, row.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollPresenterBringIntoViewPage: the same request as the ScrollView page, plus the
// event the presenter raises while answering it.
//
// BringingIntoView fires before the view moves and carries the offsets it is about to
// go to, which is the hook for overriding or cancelling the request.
func buildScrollPresenterBringIntoViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	inner, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := inner.SetOrientation(uixaml.OrientationVertical); err != nil {
		return nil, err
	}
	var targets []*uixaml.Grid
	for i := 0; i < 12; i++ {
		band, err := colouredBand(520, 140, fmt.Sprintf("Target %d", i+1), bandColours[i%len(bandColours)])
		if err != nil {
			return nil, err
		}
		if err := app.Append(inner.AsPanel, band.AsUIElement); err != nil {
			return nil, err
		}
		targets = append(targets, band)
	}
	presenter, err := newScrollPresenter(420, 240, inner.AsUIElement)
	if err != nil {
		return nil, err
	}

	status, err := label("No bring-into-view request yet.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(presenter.AddBringingIntoView,
		uixaml.NewTypedEventHandlerOfScrollPresenterAndScrollingBringingIntoViewEventArgs,
		func(_ *uixaml.IScrollPresenter, args *uixaml.IScrollingBringingIntoViewEventArgs) {
			horizontal, err := args.TargetHorizontalOffset()
			if err != nil {
				_ = status.SetText("Reading the target offset failed: " + err.Error())
				return
			}
			vertical, err := args.TargetVerticalOffset()
			if err != nil {
				_ = status.SetText("Reading the target offset failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("BringingIntoView → target offset %.0f, %.0f",
				horizontal, vertical))
		}); err != nil {
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
	row, err := buttonRow(bring(2), bring(6), bring(11))
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollPresenterManipulationModePage varies IgnoredInputKinds, which is the modern
// version of what the source page's title calls ManipulationMode.
//
// UIElement.ManipulationMode still exists and still means "which gestures raise
// manipulation events"; the presenter has its own property for which input kinds it
// declines to scroll from, and that is the one that affects scrolling.
func buildScrollPresenterManipulationModePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	kinds := []struct {
		caption string
		value   uixaml.ScrollingInputKinds
	}{
		{"Ignore nothing", uixaml.ScrollingInputKindsNone},
		{"Ignore the mouse wheel", uixaml.ScrollingInputKindsMouseWheel},
		{"Ignore touch", uixaml.ScrollingInputKindsTouch},
		{"Ignore the pen", uixaml.ScrollingInputKindsPen},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, kind := range kinds {
		caption, err := label(kind.caption)
		if err != nil {
			return nil, err
		}
		content, err := bigContent(500, 800, 4)
		if err != nil {
			return nil, err
		}
		presenter, err := newScrollPresenter(320, 150, content.AsUIElement)
		if err != nil {
			return nil, err
		}
		if err := presenter.SetIgnoredInputKinds(kind.value); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, presenter.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ScrollPresenterAccessibilityPage reads the presenter's automation peer.
//
// The peer is what a screen reader drives, and the source checks that a presenter
// reports itself as scrollable through it. FrameworkElementAutomationPeer.FromElement
// is the supported route to an existing peer.
func buildScrollPresenterAccessibilityPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(600, 1000, 5)
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(400, 220, content.AsUIElement)
	if err != nil {
		return nil, err
	}

	status, err := label("The automation peer is created on the first request for it.")
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Read the automation peer", func() {
			err := app.With(presenter.AsUIElement, func(element *uixaml.IUIElement) error {
				// FromElement takes a UIElement, not a FrameworkElement: a peer can
				// exist for anything in the visual tree.
				statics, err := uixaml.FrameworkElementAutomationPeerStatics()
				if err != nil {
					return err
				}
				defer statics.Release()
				peer, err := statics.FromElement(element)
				if err != nil {
					return err
				}
				if peer == nil {
					_ = status.SetText("No peer has been created for this presenter yet.")
					return nil
				}
				defer peer.Release()
				name, err := peer.GetClassName()
				if err != nil {
					return err
				}
				_ = status.SetText("Automation peer class name: " + name)
				return nil
			})
			if err != nil {
				_ = status.SetText("Reading the peer failed: " + err.Error())
			}
		})
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollPresenterMousePanningPage: scrolling driven by a mouse drag rather than a wheel.
//
// Panning with a mouse is off by default because a drag usually means selection. The
// source enables it and checks the presenter responds; here the same is done through
// ManipulationMode, which is what decides whether a drag becomes a manipulation at all.
func buildScrollPresenterMousePanningPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(800, 1200, 6)
	if err != nil {
		return nil, err
	}
	presenter, err := newScrollPresenter(420, 260, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		presenter.SetContentOrientation(uixaml.ScrollingContentOrientationBoth),
		app.With(presenter.AsUIElement, func(element *uixaml.IUIElement) error {
			return element.SetManipulationMode(
				uixaml.ManipulationModesTranslateX | uixaml.ManipulationModesTranslateY |
					uixaml.ManipulationModesTranslateInertia)
		}),
	); err != nil {
		return nil, err
	}
	note, err := label("ManipulationMode allows translation on both axes with inertia.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, presenter.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollPresenterLeakDetectionPage creates and drops presenters on demand.
//
// The source's version watches for the native objects surviving; that needs the test
// hooks, which do not ship. What ports is the half that does not: creating a presenter
// with content, releasing every reference to it, and reporting that the cycle completed
// — which is what the leak check drives, and which exercises the projection's own
// release paths.
func buildScrollPresenterLeakDetectionPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	created := 0
	status, err := label("No presenters created yet.")
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Create and drop 10 presenters", func() {
			for i := 0; i < 10; i++ {
				content, err := bigContent(400, 600, 3)
				if err != nil {
					_ = status.SetText("Creating content failed: " + err.Error())
					return
				}
				presenter, err := newScrollPresenter(200, 120, content.AsUIElement)
				if err != nil {
					content.Release()
					_ = status.SetText("Creating a presenter failed: " + err.Error())
					return
				}
				// Neither is in a tree, so these are the only references.
				presenter.Release()
				content.Release()
				created++
			}
			_ = status.SetText(fmt.Sprintf("%d presenters created and released.", created))
		})
	})
	if err != nil {
		return nil, err
	}
	note, err := label("The source watches for the native objects outliving their " +
		"references, which needs MUXControlsTestHooks — not in the shipped metadata. " +
		"What runs here is the create-and-release cycle itself.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, status.AsUIElement, row.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}
