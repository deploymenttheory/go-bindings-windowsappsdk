//go:build windows && amd64

package pages

// The AnnotatedScrollBar pages.
//
// Sources: controls/dev/AnnotatedScrollBar/TestUI/*Page.xaml
//
// AnnotatedScrollBar is a scroll bar carrying LABELS at known offsets — the "A", "B",
// "C" markers down the side of a long alphabetical list. It is not a RangeBase and not a
// ScrollBar: it PROVIDES an IScrollController, and something else consumes it. That
// separation is the whole design, and it is what the four source pages test from
// different sides.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

func init() {
	register(Page{Control: "AnnotatedScrollBar", Name: "AnnotatedScrollBarPage", Build: buildAnnotatedScrollBarPage})
	register(Page{Control: "AnnotatedScrollBar", Name: "AnnotatedScrollBarSummaryPage", Build: buildAnnotatedScrollBarSummaryPage})
	register(Page{Control: "AnnotatedScrollBar", Name: "AnnotatedScrollBarIScrollControllerPage", Build: buildAnnotatedScrollBarIScrollControllerPage})
	register(Page{Control: "AnnotatedScrollBar", Name: "AnnotatedScrollBarRangeBasePage", Build: buildAnnotatedScrollBarRangeBasePage})
}

func buildAnnotatedScrollBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return navigationIndex("AnnotatedScrollBar", []string{
		"AnnotatedScrollBarSummaryPage — labels, templates and the Scrolling event",
		"AnnotatedScrollBarIScrollControllerPage — driving a ScrollView through its controller",
		"AnnotatedScrollBarRangeBasePage — the ScrollViewer adapter, and why it is not a RangeBase",
	})
}

// labelledScrollBar builds an AnnotatedScrollBar with labels at fixed offsets.
//
// A label is created through its activation factory rather than a plain constructor,
// because it takes its content and its offset at construction: an
// AnnotatedScrollBarLabel with no offset would have nowhere to sit.
func labelledScrollBar(captions []string, spacing float64) (*uixaml.AnnotatedScrollBar, error) {
	bar, err := uixaml.NewAnnotatedScrollBar()
	if err != nil {
		return nil, err
	}
	labels, err := bar.Labels()
	if err != nil {
		return nil, err
	}
	defer labels.Release()

	for index, caption := range captions {
		content, err := app.Box(caption)
		if err != nil {
			return nil, err
		}
		item, err := uixaml.CreateInstanceAnnotatedScrollBarLabel(content, float64(index)*spacing)
		content.Release()
		if err != nil {
			return nil, err
		}
		entry, err := item.AsAnnotatedScrollBarLabel()
		item.Release()
		if err != nil {
			return nil, err
		}
		err = labels.Append(entry)
		entry.Release()
		if err != nil {
			return nil, err
		}
	}
	return bar, nil
}

// AnnotatedScrollBarSummaryPage: the labels, the two templates, and the events.
//
// LabelTemplate and DetailLabelTemplate are IElementFactory, and a DataTemplate IS one —
// the interface is what lets a template and a hand-written factory be interchangeable.
// So the template is loaded as markup and queried for the factory interface, rather than
// being a different kind of object.
func buildAnnotatedScrollBarSummaryPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	bar, err := labelledScrollBar(
		[]string{"A", "B", "C", "D", "E", "F", "G", "H"}, 200)
	if err != nil {
		return nil, err
	}
	if err := bar.SetSmallChange(50); err != nil {
		return nil, err
	}

	template, err := app.LoadMarkup[uixaml.IDataTemplate](
		app.Markup(`<DataTemplate><TextBlock Text="{Binding Content}" Margin="4,0"/></DataTemplate>`),
		&uixaml.IID_IDataTemplate)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	factory, err := winrt.QueryInterface[uixaml.IElementFactory](
		unsafe.Pointer(template), &uixaml.IID_IElementFactory)
	if err != nil {
		return nil, fmt.Errorf("a DataTemplate should be an IElementFactory: %w", err)
	}
	defer factory.Release()
	if err := bar.SetLabelTemplate(factory); err != nil {
		return nil, err
	}

	status, err := label("No interaction yet.")
	if err != nil {
		return nil, err
	}

	// Scrolling reports where the user has dragged to, in the same offset space the
	// labels were placed in.
	if _, err := app.On(bar.AddScrolling,
		uixaml.NewTypedEventHandlerOfAnnotatedScrollBarAndAnnotatedScrollBarScrollingEventArgs,
		func(_ *uixaml.IAnnotatedScrollBar, args *uixaml.IAnnotatedScrollBarScrollingEventArgs) {
			offset, err := args.ScrollOffset()
			if err != nil {
				_ = status.SetText("Reading the offset failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("Scrolling to offset %.0f", offset))
		}); err != nil {
		return nil, err
	}

	// DetailLabelRequested is the tooltip beside the thumb. The control asks for the
	// content each time rather than holding a list, so this is a callback and not a
	// property — the whole point being that the detail depends on where you are.
	if _, err := app.On(bar.AddDetailLabelRequested,
		uixaml.NewTypedEventHandlerOfAnnotatedScrollBarAndAnnotatedScrollBarDetailLabelRequestedEventArgs,
		func(_ *uixaml.IAnnotatedScrollBar, args *uixaml.IAnnotatedScrollBarDetailLabelRequestedEventArgs) {
			offset, err := args.ScrollOffset()
			if err != nil {
				return
			}
			content, err := app.Box(fmt.Sprintf("At %.0f", offset))
			if err != nil {
				return
			}
			defer content.Release()
			_ = args.SetContent(content)
		}); err != nil {
		return nil, err
	}

	if err := app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(360), frame.SetWidth(120))
	}); err != nil {
		return nil, err
	}

	note, err := label("Eight labels, 200 apart, with a LabelTemplate and a detail label.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, status.AsUIElement, bar.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// AnnotatedScrollBarIScrollControllerPage is the one that wires it up for real: a
// ScrollView scrolled by the bar instead of by its own scroll bar.
//
// This needs no Go-implemented IScrollController. The bar HAS one — ScrollController is
// a read-only property returning the interface — and ScrollPresenter takes it. So the
// two halves of the contract are both native, and the whole page is four calls.
func buildAnnotatedScrollBarIScrollControllerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(560, 2400, 12)
	if err != nil {
		return nil, err
	}
	view, err := newScrollView(560, 380, content.AsUIElement)
	if err != nil {
		return nil, err
	}
	if err := view.SetVerticalScrollBarVisibility(uixaml.ScrollingScrollBarVisibilityHidden); err != nil {
		return nil, err
	}

	bar, err := labelledScrollBar(
		[]string{"Start", "Quarter", "Half", "Three quarters", "End"}, 480)
	if err != nil {
		return nil, err
	}
	if err := app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(140), frame.SetHeight(380))
	}); err != nil {
		return nil, err
	}

	// The presenter is inside the ScrollView's template, so it exists only after the
	// template has been applied — which is why the wiring happens on Loaded rather than
	// here. Before then ScrollPresenter is null and there is nothing to attach to.
	controller, err := bar.ScrollController()
	if err != nil {
		return nil, err
	}
	status, err := label("Waiting for the ScrollView's template…")
	if err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				presenter, err := view.ScrollPresenter()
				if err != nil {
					_ = status.SetText("No ScrollPresenter: " + err.Error())
					return
				}
				if presenter == nil {
					_ = status.SetText("The template has not produced a ScrollPresenter.")
					return
				}
				defer presenter.Release()
				if err := presenter.SetVerticalScrollController(controller); err != nil {
					_ = status.SetText("Attaching the controller failed: " + err.Error())
					return
				}
				_ = status.SetText("The AnnotatedScrollBar is driving the ScrollView.")
			})
		return addErr
	}); err != nil {
		controller.Release()
		return nil, err
	}

	row, err := uixaml.NewStackPanel()
	if err != nil {
		controller.Release()
		return nil, err
	}
	if err := app.All(
		row.SetOrientation(uixaml.OrientationHorizontal),
		row.SetSpacing(8),
		app.Append(row.AsPanel, view.AsUIElement, bar.AsUIElement),
	); err != nil {
		controller.Release()
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, row.AsUIElement)
	if err != nil {
		controller.Release()
		return nil, err
	}
	return panel.AsUIElement()
}

// AnnotatedScrollBarRangeBasePage: the source drives a ScrollViewer through an adapter
// rather than a ScrollView through the controller.
//
// The name says RangeBase and the control is not one — AnnotatedScrollBar has no
// Minimum, Maximum or Value. That is the page's actual subject: ScrollViewer is the UWP
// control and knows nothing about IScrollController, so connecting the two means writing
// an adapter that turns the bar's Scrolling event into ChangeView calls and the
// viewer's ViewChanged back into the bar's offsets. That adapter is what this ports.
func buildAnnotatedScrollBarRangeBasePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	content, err := bigContent(520, 2000, 10)
	if err != nil {
		return nil, err
	}
	viewer, err := uixaml.NewScrollViewer()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		// ScrollViewer is a ContentControl, so its content is IInspectable rather than
		// UIElement — the UWP control accepts anything and templates it, where
		// ScrollView takes an element directly.
		app.With(content.AsUIElement, func(element *uixaml.IUIElement) error {
			return app.With(viewer.AsContentControl, func(control *uixaml.IContentControl) error {
				return control.SetContent((*syswinrt.IInspectable)(unsafe.Pointer(element)))
			})
		}),
		app.With(viewer.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(520), frame.SetHeight(360))
		}),
		viewer.SetVerticalScrollBarVisibility(uixaml.ScrollBarVisibilityHidden),
	); err != nil {
		return nil, err
	}

	bar, err := labelledScrollBar(
		[]string{"Top", "Second", "Third", "Fourth", "Bottom"}, 400)
	if err != nil {
		return nil, err
	}
	if err := app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(140), frame.SetHeight(360))
	}); err != nil {
		return nil, err
	}

	status, err := label("Drag the bar to scroll the ScrollViewer beside it.")
	if err != nil {
		return nil, err
	}

	// The adapter, one direction: the bar reports an offset, the viewer is told to go
	// there. ChangeView's arguments are IReference<Double> — nullable, so "leave this
	// axis alone" is expressible — which is why the horizontal one and the zoom factor
	// are null here.
	if _, err := app.On(bar.AddScrolling,
		uixaml.NewTypedEventHandlerOfAnnotatedScrollBarAndAnnotatedScrollBarScrollingEventArgs,
		func(_ *uixaml.IAnnotatedScrollBar, args *uixaml.IAnnotatedScrollBarScrollingEventArgs) {
			offset, err := args.ScrollOffset()
			if err != nil {
				_ = status.SetText("Reading the offset failed: " + err.Error())
				return
			}
			vertical, err := app.NewReference[uixaml.IReferenceOfDouble](
				offset, &uixaml.IID_IReferenceOfDouble,
				"Windows.Foundation.IReference`1<Double>")
			if err != nil {
				_ = status.SetText("Boxing the offset failed: " + err.Error())
				return
			}
			defer vertical.Close()
			changed, err := viewer.ChangeViewWithOptionalAnimation(nil, vertical.Value(), nil, true)
			if err != nil {
				_ = status.SetText("ChangeView failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("Adapter: offset %.0f, ChangeView accepted=%v",
				offset, changed))
		}); err != nil {
		return nil, err
	}

	row, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		row.SetOrientation(uixaml.OrientationHorizontal),
		row.SetSpacing(8),
		app.Append(row.AsPanel, viewer.AsUIElement, bar.AsUIElement),
	); err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}
