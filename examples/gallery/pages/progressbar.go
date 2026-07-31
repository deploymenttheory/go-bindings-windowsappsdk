//go:build windows && amd64

package pages

// Ported from controls/dev/ProgressBar/TestUI/ProgressBarPage.xaml.
//
// Worth having early for a reason beyond coverage: ProgressBar is one of the controls
// that used to terminate the process at layout, because its default style lives in
// WinUI's theme dictionaries and nothing could reach them. It renders now, and this page
// is what keeps that true.

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "ProgressBar", Name: "ProgressBarPage", Build: buildProgressBarPage})
}

func buildProgressBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	// The source shows the four states a ProgressBar can be in. Each is a RangeBase
	// underneath, so Value and Maximum come from two classes up.
	type barCase struct {
		caption       string
		value         float64
		indeterminate bool
		paused        bool
		errored       bool
	}
	cases := []barCase{
		{"Determinate, 40%", 40, false, false, false},
		{"Indeterminate", 0, true, false, false},
		{"Paused", 60, false, true, false},
		{"Error", 60, false, false, true},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, testCase := range cases {
		caption, err := label(testCase.caption)
		if err != nil {
			return nil, err
		}
		bar, err := uixaml.NewProgressBar()
		if err != nil {
			return nil, err
		}
		err = app.All(
			bar.SetIsIndeterminate(testCase.indeterminate),
			bar.SetShowPaused(testCase.paused),
			bar.SetShowError(testCase.errored),
			// Value and Maximum are RangeBase's, two classes above ProgressBar.
			app.With(bar.AsRangeBase, func(rangeBase *uixaml.IRangeBase) error {
				return app.All(
					rangeBase.SetMaximum(100),
					rangeBase.SetValue(testCase.value),
				)
			}),
			app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return frame.SetWidth(300)
			}),
		)
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, bar.AsUIElement)
	}

	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}
