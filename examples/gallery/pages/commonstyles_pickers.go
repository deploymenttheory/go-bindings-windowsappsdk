//go:build windows && amd64

package pages

// The date and time pickers, plus the two media surfaces.
//
// Sources: controls/dev/CommonStyles/TestUI/{CalendarView,CalendarDatePicker,DatePicker,
// TimePicker,MediaTransportControls,InkToolbar}Page.xaml

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "CalendarViewPage", Build: buildCalendarViewPage})
	register(Page{Control: "CommonStyles", Name: "CalendarDatePickerPage", Build: buildCalendarDatePickerPage})
	register(Page{Control: "CommonStyles", Name: "DatePickerPage", Build: buildDatePickerPage})
	register(Page{Control: "CommonStyles", Name: "TimePickerPage", Build: buildTimePickerPage})
	register(Page{Control: "CommonStyles", Name: "MediaTransportControlsPage", Build: buildMediaTransportControlsPage})
	// InkToolbar and InkCanvas are absent from the Windows App SDK metadata entirely —
	// not skipped by this projection, not present to skip. They were UWP controls and
	// did not come across to WinUI 3, the same way SearchBox did not. Recorded rather
	// than omitted so the gap is visible.
	register(Page{Control: "CommonStyles", Name: "InkToolbarPage",
		Unmappable: "InkToolbar and InkCanvas are not Windows App SDK types; they were " +
			"UWP controls that did not come across to WinUI 3"})
}

// CalendarViewPage: the calendar in each of the display modes the source steps through.
func buildCalendarViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	modes := []struct {
		caption string
		value   uixaml.CalendarViewDisplayMode
	}{
		{"Month", uixaml.CalendarViewDisplayModeMonth},
		{"Year", uixaml.CalendarViewDisplayModeYear},
		{"Decade", uixaml.CalendarViewDisplayModeDecade},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range modes {
		caption, err := label(mode.caption)
		if err != nil {
			return nil, err
		}
		view, err := uixaml.NewCalendarView()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			view.SetDisplayMode(mode.value),
			view.SetSelectionMode(uixaml.CalendarViewSelectionModeSingle),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, view.AsUIElement)
	}
	panel, err := stack(12, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func buildCalendarDatePickerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	picker, err := uixaml.NewCalendarDatePicker()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		picker.SetPlaceholderText("Pick a date"),
		picker.SetIsTodayHighlighted(true),
	); err != nil {
		return nil, err
	}
	caption, err := label("CalendarDatePicker")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, picker.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildDatePickerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, useHeader := range []bool{false, true} {
		picker, err := uixaml.NewDatePicker()
		if err != nil {
			return nil, err
		}
		if useHeader {
			header, err := app.Box("With a header")
			if err != nil {
				return nil, err
			}
			err = picker.SetHeader(header)
			header.Release()
			if err != nil {
				return nil, err
			}
		}
		if err := picker.SetYearVisible(true); err != nil {
			return nil, err
		}
		children = append(children, picker.AsUIElement)
	}
	caption, err := label("DatePicker, plain and with a header")
	if err != nil {
		return nil, err
	}
	panel, err := stack(12, append([]func() (*uixaml.IUIElement, error){caption.AsUIElement}, children...)...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildTimePickerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, clock := range []string{"12HourClock", "24HourClock"} {
		caption, err := label(clock)
		if err != nil {
			return nil, err
		}
		picker, err := uixaml.NewTimePicker()
		if err != nil {
			return nil, err
		}
		if err := picker.SetClockIdentifier(clock); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, picker.AsUIElement)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// MediaTransportControlsPage: the transport bar on its own.
//
// The source hosts it inside a MediaPlayerElement with a real video. There is no media
// to point at here, so the controls are shown standalone — which is what the page is
// checking anyway, since it is a styling test rather than a playback one.
func buildMediaTransportControlsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	controls, err := uixaml.NewMediaTransportControls()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		controls.SetIsZoomButtonVisible(false),
		controls.SetIsVolumeButtonVisible(true),
		controls.SetIsSeekBarVisible(true),
	); err != nil {
		return nil, err
	}
	caption, err := label("MediaTransportControls, without a media source")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, controls.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}
