//go:build windows && amd64

package pages

// The items-control pages, plus the input controls that share their shape.
//
// Sources: controls/dev/CommonStyles/TestUI/{ItemsControl,ListView,GridView,FlipView,
// Pivot,CheckBox,Slider,ScrollViewer,TextControls}Page.xaml

import (
	"fmt"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "ItemsControlPage", Build: buildItemsControlPage})
	register(Page{Control: "CommonStyles", Name: "ListViewPage", Build: buildListViewPage})
	register(Page{Control: "CommonStyles", Name: "GridViewPage", Build: buildGridViewPage})
	register(Page{Control: "CommonStyles", Name: "FlipViewPage", Build: buildFlipViewPage})
	register(Page{Control: "CommonStyles", Name: "PivotPage", Build: buildPivotPage})
	register(Page{Control: "CommonStyles", Name: "CheckBoxPage", Build: buildCheckBoxPage})
	register(Page{Control: "CommonStyles", Name: "SliderPage", Build: buildSliderPage})
	register(Page{Control: "CommonStyles", Name: "ScrollViewerPage", Build: buildScrollViewerPage})
	register(Page{Control: "CommonStyles", Name: "TextControlsPage", Build: buildTextControlsPage})
}

// fillItems appends boxed strings to any ItemsControl's Items.
//
// Items is an IObservableVector<Object>, which carries only its VectorChanged event, so
// adding goes through the IVector it requires — the accessor that made list controls
// usable at all.
func fillItems(control unsafe.Pointer, values []string) error {
	items, err := winrt.QueryInterface[uixaml.IItemsControl](control, &uixaml.IID_IItemsControl)
	if err != nil {
		return fmt.Errorf("not an ItemsControl: %w", err)
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

	for _, value := range values {
		boxed, err := app.Box(value)
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
}

var sampleItems = []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}

func buildItemsControlPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	control, err := uixaml.NewItemsControl()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(control), sampleItems); err != nil {
		return nil, err
	}
	caption, err := label("ItemsControl: items with no selection or chrome")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func buildListViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewListView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(view), sampleItems); err != nil {
		return nil, err
	}
	// SelectionMode is ListViewBase's, one class above ListView.
	if err := app.With(view.AsListViewBase, func(base *uixaml.IListViewBase) error {
		return base.SetSelectionMode(uixaml.ListViewSelectionModeMultiple)
	}); err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(240)
	}); err != nil {
		return nil, err
	}
	caption, err := label("ListView, multiple selection")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildGridViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewGridView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(view), sampleItems); err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(240)
	}); err != nil {
		return nil, err
	}
	caption, err := label("GridView")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildFlipViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	view, err := uixaml.NewFlipView()
	if err != nil {
		return nil, err
	}
	if err := fillItems(unsafe.Pointer(view), sampleItems); err != nil {
		return nil, err
	}
	if err := app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(200), frame.SetWidth(320))
	}); err != nil {
		return nil, err
	}
	caption, err := label("FlipView")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// PivotPage: a Pivot of PivotItems. Unlike the list controls the children are real
// controls rather than boxed values, so they go in as objects.
func buildPivotPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	pivot, err := uixaml.NewPivot()
	if err != nil {
		return nil, err
	}
	items, err := winrt.QueryInterface[uixaml.IItemsControl](
		unsafe.Pointer(pivot), &uixaml.IID_IItemsControl)
	if err != nil {
		return nil, err
	}
	defer items.Release()
	observable, err := items.Items()
	if err != nil {
		return nil, err
	}
	defer observable.Release()
	collection, err := observable.AsVectorOfObject()
	if err != nil {
		return nil, err
	}
	defer collection.Release()

	for _, name := range []string{"First", "Second", "Third"} {
		item, err := uixaml.NewPivotItem()
		if err != nil {
			return nil, err
		}
		header, err := app.Box(name)
		if err != nil {
			return nil, err
		}
		body, err := label(name + " content")
		if err != nil {
			header.Release()
			return nil, err
		}
		err = app.All(
			item.SetHeader(header),
			app.With(body.AsUIElement, func(element *uixaml.IUIElement) error {
				return app.With(item.AsContentControl, func(content *uixaml.IContentControl) error {
					return content.SetContent(&element.IInspectable)
				})
			}),
		)
		header.Release()
		if err != nil {
			return nil, err
		}
		if err := collection.Append(&item.IPivotItem.IInspectable); err != nil {
			return nil, err
		}
	}
	if err := app.With(pivot.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(220)
	}); err != nil {
		return nil, err
	}
	return pivot.AsUIElement()
}

// CheckBoxPage: the three states a CheckBox can be in, which is what IsThreeState and a
// nil IsChecked are for.
func buildCheckBoxPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	type checkCase struct {
		caption    string
		checked    any // nil is the indeterminate state
		threeState bool
	}
	cases := []checkCase{
		{"Unchecked", false, false},
		{"Checked", true, false},
		{"Indeterminate", nil, true},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, testCase := range cases {
		box, err := uixaml.NewCheckBox()
		if err != nil {
			return nil, err
		}
		if err := app.SetContent(box.AsContentControl, testCase.caption); err != nil {
			return nil, err
		}
		if err := app.With(box.AsToggleButton, func(toggle *uixaml.IToggleButton) error {
			if err := toggle.SetIsThreeState(testCase.threeState); err != nil {
				return err
			}
			if testCase.checked == nil {
				// nil IS the indeterminate state; no reference to box.
				return toggle.SetIsChecked(nil)
			}
			value, err := app.BoxAs[uixaml.IReferenceOfBool](testCase.checked, &uixaml.IID_IReferenceOfBool)
			if err != nil {
				return err
			}
			defer value.Release()
			return toggle.SetIsChecked(value)
		}); err != nil {
			return nil, err
		}
		children = append(children, box.AsUIElement)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildSliderPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	type sliderCase struct {
		caption     string
		orientation uixaml.Orientation
		ticks       uixaml.TickPlacement
	}
	cases := []sliderCase{
		{"Horizontal", uixaml.OrientationHorizontal, uixaml.TickPlacementNone},
		{"With tick marks", uixaml.OrientationHorizontal, uixaml.TickPlacementOutside},
		{"Vertical", uixaml.OrientationVertical, uixaml.TickPlacementNone},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, testCase := range cases {
		caption, err := label(testCase.caption)
		if err != nil {
			return nil, err
		}
		slider, err := uixaml.NewSlider()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			slider.SetOrientation(testCase.orientation),
			slider.SetTickPlacement(testCase.ticks),
			slider.SetTickFrequency(10),
			// Minimum, Maximum and Value are RangeBase's, two classes up.
			app.With(slider.AsRangeBase, func(rangeBase *uixaml.IRangeBase) error {
				return app.All(
					rangeBase.SetMinimum(0),
					rangeBase.SetMaximum(100),
					rangeBase.SetValue(40),
				)
			}),
			app.With(slider.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				if testCase.orientation == uixaml.OrientationVertical {
					return frame.SetHeight(140)
				}
				return frame.SetWidth(280)
			}),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, slider.AsUIElement)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// ScrollViewerPage: content larger than the viewport, with both bars showing.
func buildScrollViewerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for i := 0; i < 30; i++ {
		line, err := label(fmt.Sprintf("Line %d — content taller than the viewport", i+1))
		if err != nil {
			return nil, err
		}
		children = append(children, line.AsUIElement)
	}
	inner, err := stack(4, children...)
	if err != nil {
		return nil, err
	}

	viewer, err := uixaml.NewScrollViewer()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		viewer.SetHorizontalScrollBarVisibility(uixaml.ScrollBarVisibilityAuto),
		viewer.SetVerticalScrollBarVisibility(uixaml.ScrollBarVisibilityVisible),
		app.With(viewer.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(240)
		}),
		app.With(inner.AsUIElement, func(element *uixaml.IUIElement) error {
			return app.With(viewer.AsContentControl, func(host *uixaml.IContentControl) error {
				return host.SetContent(&element.IInspectable)
			})
		}),
	); err != nil {
		return nil, err
	}
	return viewer.AsUIElement()
}

// TextControlsPage: the text-input family.
//
// These are the controls that used to terminate the process at layout, before the
// derived application and the resource index. The page is here as much to keep that
// fixed as to demonstrate them.
func buildTextControlsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	box, err := uixaml.NewTextBox()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		box.SetPlaceholderText("TextBox"),
		box.SetText("Editable text"),
	); err != nil {
		return nil, err
	}

	multiline, err := uixaml.NewTextBox()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		multiline.SetAcceptsReturn(true),
		multiline.SetText("A TextBox\nover several\nlines"),
		app.With(multiline.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetHeight(90)
		}),
	); err != nil {
		return nil, err
	}

	password, err := uixaml.NewPasswordBox()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		password.SetPlaceholderText("PasswordBox"),
		password.SetPassword("secret"),
	); err != nil {
		return nil, err
	}

	rich, err := uixaml.NewRichEditBox()
	if err != nil {
		return nil, err
	}
	if err := app.With(rich.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHeight(90)
	}); err != nil {
		return nil, err
	}

	number, err := uixaml.NewNumberBox()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		number.SetValue(42),
		number.SetSpinButtonPlacementMode(uixaml.NumberBoxSpinButtonPlacementModeInline),
	); err != nil {
		return nil, err
	}

	captions := []string{"TextBox", "TextBox, multiline", "PasswordBox", "RichEditBox", "NumberBox"}
	elements := []func() (*uixaml.IUIElement, error){
		box.AsUIElement, multiline.AsUIElement, password.AsUIElement,
		rich.AsUIElement, number.AsUIElement,
	}
	var children []func() (*uixaml.IUIElement, error)
	for i, element := range elements {
		caption, err := label(captions[i])
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, element)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}
