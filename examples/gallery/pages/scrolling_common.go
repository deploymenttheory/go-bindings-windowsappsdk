//go:build windows && amd64

package pages

// Helpers shared by the scrolling batch.
//
// Every one of these pages needs the same two things: content larger than its viewport,
// and a readout of where the view currently is. The sources build both inline, in markup,
// once per page.

import (
	"fmt"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlshapes "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/shapes"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

// bandColours are what the sources use to make scrolling visible: without contrasting
// blocks, a view that has moved looks exactly like one that has not.
var bandColours = []wrtui.Color{
	{A: 255, R: 200, G: 60, B: 60},
	{A: 255, R: 60, G: 160, B: 80},
	{A: 255, R: 60, G: 100, B: 200},
	{A: 255, R: 210, G: 160, B: 40},
	{A: 255, R: 140, G: 70, B: 180},
}

// bigContent builds content larger than any viewport it will be put in: a stack of
// numbered, coloured bands.
//
// The sources use a Rectangle with a fixed Width and Height, or an Image of a known
// size. Bands do the same job and say where you are while doing it.
func bigContent(width, height float64, bands int) (*uixaml.StackPanel, error) {
	panel, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := panel.SetOrientation(uixaml.OrientationVertical); err != nil {
		return nil, err
	}
	for i := 0; i < bands; i++ {
		band, err := colouredBand(width, height/float64(bands), fmt.Sprintf("Band %d", i+1),
			bandColours[i%len(bandColours)])
		if err != nil {
			return nil, err
		}
		if err := app.Append(panel.AsPanel, band.AsUIElement); err != nil {
			return nil, err
		}
	}
	return panel, nil
}

// colouredBand is one block of the above: a Grid so the caption sits over the fill.
func colouredBand(width, height float64, caption string, colour wrtui.Color) (*uixaml.Grid, error) {
	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	rectangle, err := uixamlshapes.NewRectangle()
	if err != nil {
		return nil, err
	}
	brush, err := solidBrush(colour)
	if err != nil {
		return nil, err
	}
	// Fill is IShape's, not Rectangle's own — the shape hierarchy puts every paint
	// property on the base, so a Rectangle reaches it the same way a Path does.
	err = app.With(rectangle.AsShape, func(shape *uixamlshapes.IShape) error {
		return shape.SetFill(brush)
	})
	brush.Release()
	if err != nil {
		return nil, err
	}

	text, err := label(caption)
	if err != nil {
		return nil, err
	}
	white, err := solidBrush(wrtui.Color{A: 255, R: 255, G: 255, B: 255})
	if err != nil {
		return nil, err
	}
	err = text.SetForeground(white)
	white.Release()
	if err != nil {
		return nil, err
	}

	if err := app.All(
		app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(width), frame.SetHeight(height))
		}),
		app.Append(grid.AsPanel, rectangle.AsUIElement, text.AsUIElement),
	); err != nil {
		return nil, err
	}
	return grid, nil
}

// viewReadout is the "HorizontalOffset / VerticalOffset / ZoomFactor" line every one of
// these pages carries. Refresh updates it from whatever the page is driving.
type viewReadout struct {
	block *uixaml.TextBlock
}

func newViewReadout() (*viewReadout, error) {
	block, err := label("Offset 0, 0 — zoom 1.00")
	if err != nil {
		return nil, err
	}
	return &viewReadout{block: block}, nil
}

func (r *viewReadout) set(horizontal, vertical float64, zoom float32) {
	_ = r.block.SetText(fmt.Sprintf("Offset %.0f, %.0f — zoom %.2f", horizontal, vertical, zoom))
}

func (r *viewReadout) fail(err error) {
	_ = r.block.SetText("Failed: " + err.Error())
}

func (r *viewReadout) element() (*uixaml.IUIElement, error) {
	return r.block.AsUIElement()
}

// button builds a captioned button whose click runs action, which is the shape every
// one of these pages repeats for its API-exercising buttons.
func button(caption string, action func()) (*uixaml.Button, error) {
	control, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(control.AsContentControl, caption); err != nil {
		return nil, err
	}
	if err := app.With(control.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { action() })
		return addErr
	}); err != nil {
		return nil, err
	}
	return control, nil
}

// buttonRow lays captioned actions out horizontally, the way these pages group their
// controls above the view they drive.
func buttonRow(actions ...func() (*uixaml.Button, error)) (*uixaml.StackPanel, error) {
	panel, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		panel.SetOrientation(uixaml.OrientationHorizontal),
		panel.SetSpacing(6),
	); err != nil {
		return nil, err
	}
	for _, make := range actions {
		control, err := make()
		if err != nil {
			return nil, err
		}
		if err := app.Append(panel.AsPanel, control.AsUIElement); err != nil {
			return nil, err
		}
	}
	return panel, nil
}

// navigationIndex ports the three index pages — ScrollViewPage, ScrollPresenterPage and
// AnnotatedScrollBarPage — which in the source are menus of buttons that navigate to the
// other pages in their family.
//
// The gallery's own list is that navigation, so the buttons would be a second one that
// does nothing. What is kept is the list itself, which is the page's actual content: the
// family's pages, named, so the index reads as an index.
//
// The source also carries an OutputDebugString level ComboBox on each. That drives
// MUXControlsTestHooks, a test-only interface not in the shipped metadata, so the combo
// is present and inert — which is what it would be here in any case, since there is no
// debug output to route.
func navigationIndex(family string, entries []string) (*uixaml.IUIElement, error) {
	heading, err := label(family + " test pages")
	if err != nil {
		return nil, err
	}
	note, err := label("Pick any of these from the gallery's list on the left. In the source " +
		"this page is a menu of navigation buttons; here the list is that menu.")
	if err != nil {
		return nil, err
	}
	levels, err := newComboBox("OutputDebugString level", []string{"None", "Info", "Verbose"})
	if err != nil {
		return nil, err
	}

	children := []func() (*uixaml.IUIElement, error){
		heading.AsUIElement, note.AsUIElement, levels.AsUIElement,
	}
	for _, entry := range entries {
		item, err := label("  • " + entry)
		if err != nil {
			return nil, err
		}
		children = append(children, item.AsUIElement)
	}
	panel, err := stack(6, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}
