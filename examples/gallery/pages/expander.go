//go:build windows && amd64

package pages

// Ported from controls/dev/Expander/TestUI/ExpanderPage.xaml.
//
// The source wraps a StackPanel of Expanders in a ScrollViewer and gives the panel a
// ChildrenTransitions collection. The transitions are kept: they are a TransitionCollection
// of RepositionThemeTransition, which is a plain object graph and ports directly.
//
// x:Name is dropped throughout, here and in every page: a Go variable is what x:Name is
// for. Automation names are dropped unless a page's behaviour depends on one.

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "Expander", Name: "ExpanderPage", Build: buildExpanderPage})
}

func buildExpanderPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	panel, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := panel.SetOrientation(uixaml.OrientationVertical); err != nil {
		return nil, err
	}
	if err := app.With(panel.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentLeft)
	}); err != nil {
		return nil, err
	}

	// <Button Content="Dummy Button" /> — the source's focus target, kept because the
	// Expander tests tab order away from it.
	dummy, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(dummy.AsContentControl, "Dummy Button"); err != nil {
		return nil, err
	}

	// The source declares three: expanded, collapsed, and one expanding upwards.
	type expanderCase struct {
		header    string
		content   string
		expanded  bool
		direction uixaml.ExpandDirection
	}
	cases := []expanderCase{
		{"This expander is expanded by default.", "Expanded content.", true, uixaml.ExpandDirectionDown},
		{"This expander is collapsed by default.", "Collapsed content.", false, uixaml.ExpandDirectionDown},
		{"This expander expands upwards.", "Upward content.", false, uixaml.ExpandDirectionUp},
	}

	children := []func() (*uixaml.IUIElement, error){dummy.AsUIElement}
	for _, testCase := range cases {
		expander, err := uixaml.NewExpander()
		if err != nil {
			return nil, err
		}
		header, err := uixaml.NewTextBlock()
		if err != nil {
			return nil, err
		}
		body, err := uixaml.NewTextBlock()
		if err != nil {
			return nil, err
		}
		headerElement, err := header.AsUIElement()
		if err != nil {
			return nil, err
		}
		bodyElement, err := body.AsUIElement()
		if err != nil {
			return nil, err
		}
		// Header and Content are IInspectable: any object, not just a string. The source
		// puts a StackPanel of TextBlocks in the header; one TextBlock carries the same
		// shape without the noise.
		err = app.All(
			header.SetText(testCase.header),
			body.SetText(testCase.content),
			expander.SetHeader(&headerElement.IInspectable),
			expander.SetIsExpanded(testCase.expanded),
			expander.SetExpandDirection(testCase.direction),
			app.With(expander.AsContentControl, func(content *uixaml.IContentControl) error {
				return content.SetContent(&bodyElement.IInspectable)
			}),
			app.With(expander.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return app.All(
					frame.SetMargin(uixaml.Thickness{Left: 12, Top: 12, Right: 12, Bottom: 12}),
					frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentLeft),
				)
			}),
		)
		headerElement.Release()
		bodyElement.Release()
		if err != nil {
			return nil, err
		}
		children = append(children, expander.AsUIElement)
	}

	if err := app.Append(panel.AsPanel, children...); err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}
