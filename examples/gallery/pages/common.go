//go:build windows && amd64

package pages

// Shapes shared by the ported pages.
//
// Nearly every TestUI page is a ScrollViewer around a StackPanel of the control under
// test plus the switches that drive it. Writing that out per page would bury the part
// that differs, which is the part worth reading.

import (
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// scrolled wraps content in a vertically scrolling ScrollViewer, which is what almost
// every source page's root element is.
func scrolled(content func() (*uixaml.IUIElement, error)) (*uixaml.IUIElement, error) {
	viewer, err := uixaml.NewScrollViewer()
	if err != nil {
		return nil, err
	}
	if err := app.With(viewer.AsScrollViewer, func(scroll *uixaml.IScrollViewer) error {
		return app.All(
			scroll.SetHorizontalScrollBarVisibility(uixaml.ScrollBarVisibilityDisabled),
			scroll.SetVerticalScrollBarVisibility(uixaml.ScrollBarVisibilityAuto),
		)
	}); err != nil {
		return nil, err
	}
	if err := app.With(content, func(element *uixaml.IUIElement) error {
		return app.With(viewer.AsContentControl, func(host *uixaml.IContentControl) error {
			return host.SetContent(&element.IInspectable)
		})
	}); err != nil {
		return nil, err
	}
	return viewer.AsUIElement()
}

// stack builds a vertical StackPanel of the given children, the body of most pages.
func stack(spacing float64, children ...func() (*uixaml.IUIElement, error)) (*uixaml.StackPanel, error) {
	panel, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		panel.SetOrientation(uixaml.OrientationVertical),
		panel.SetSpacing(spacing),
	); err != nil {
		return nil, err
	}
	if err := app.Append(panel.AsPanel, children...); err != nil {
		return nil, err
	}
	return panel, nil
}

// label builds a TextBlock, the caption beside nearly every switch in these pages.
func label(text string) (*uixaml.TextBlock, error) {
	block, err := uixaml.NewTextBlock()
	if err != nil {
		return nil, err
	}
	if err := block.SetText(text); err != nil {
		return nil, err
	}
	return block, nil
}

// Grid lengths, spelled as the markup spells them: Auto, *, and a fixed size.
func auto() uixaml.GridLength { return uixaml.GridLength{GridUnitType: uixaml.GridUnitTypeAuto} }
func star(weight float64) uixaml.GridLength {
	return uixaml.GridLength{Value: weight, GridUnitType: uixaml.GridUnitTypeStar}
}
func px(size float64) uixaml.GridLength {
	return uixaml.GridLength{Value: size, GridUnitType: uixaml.GridUnitTypePixel}
}

// gridOf builds a Grid with the given row and column definitions, which in markup are
// the Grid.RowDefinitions and Grid.ColumnDefinitions collections.
func gridOf(rows, columns []uixaml.GridLength) (*uixaml.Grid, error) {
	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		definitions, err := grid.RowDefinitions()
		if err != nil {
			return nil, err
		}
		defer definitions.Release()
		for _, height := range rows {
			row, err := uixaml.NewRowDefinition()
			if err != nil {
				return nil, err
			}
			if err := row.SetHeight(height); err != nil {
				return nil, err
			}
			if err := definitions.Append(&row.IRowDefinition); err != nil {
				return nil, err
			}
		}
	}
	if len(columns) > 0 {
		definitions, err := grid.ColumnDefinitions()
		if err != nil {
			return nil, err
		}
		defer definitions.Release()
		for _, width := range columns {
			column, err := uixaml.NewColumnDefinition()
			if err != nil {
				return nil, err
			}
			if err := column.SetWidth(width); err != nil {
				return nil, err
			}
			if err := definitions.Append(&column.IColumnDefinition); err != nil {
				return nil, err
			}
		}
	}
	return grid, nil
}

// placement positions elements in a Grid through the Grid.Row and Grid.Column ATTACHED
// properties — the Grid.Row="1" of markup. They are static methods on Grid rather than
// members of the element, so they need the statics object and a query per element; the
// statics is fetched once and reused.
type placement struct{ statics *uixaml.IGridStatics }

func newPlacement() (*placement, error) {
	statics, err := uixaml.GridStatics()
	if err != nil {
		return nil, err
	}
	return &placement{statics: statics}, nil
}

func (p *placement) Close() {
	if p.statics != nil {
		p.statics.Release()
		p.statics = nil
	}
}

// at sets an element's Grid.Row and Grid.Column.
func (p *placement) at(element func() (*uixaml.IUIElement, error), row, column int) error {
	return app.With(element, func(target *uixaml.IUIElement) error {
		frame, err := frameworkElementOf(target)
		if err != nil {
			return err
		}
		defer frame.Release()
		return app.All(
			p.statics.SetRow(frame, int32(row)),
			p.statics.SetColumn(frame, int32(column)),
		)
	})
}

// frameworkElementOf queries an element for IFrameworkElement.
//
// IUIElement and IFrameworkElement are related by class inheritance rather than by a
// Requires clause, so there is no generated accessor between the two interfaces — only
// classes get those. Attached properties take an IFrameworkElement, so this hop is
// unavoidable wherever an element arrives as IUIElement.
func frameworkElementOf(element *uixaml.IUIElement) (*uixaml.IFrameworkElement, error) {
	return winrt.QueryInterface[uixaml.IFrameworkElement](
		unsafe.Pointer(element), &uixaml.IID_IFrameworkElement)
}

// dependencyObjectOf queries an element for IDependencyObject, which is what the
// attached-property setters on the *Service statics take — ToolTipService.SetToolTip,
// AutomationProperties.SetName and their neighbours all sit on the dependency object
// rather than on the element.
func dependencyObjectOf(element *uixaml.IUIElement) (*uixaml.IDependencyObject, error) {
	return winrt.QueryInterface[uixaml.IDependencyObject](
		unsafe.Pointer(element), &uixaml.IID_IDependencyObject)
}
