//go:build windows && amd64

package acceptance

// Does the framework actually call a VirtualizingLayout derived in Go?
//
// The four gallery pages that use one all pass the conformance suite, and that proves
// less than it appears to. Each puts its repeater inside a ScrollView with a fixed size,
// so the page measures non-zero whether or not the layout does anything — a derivation
// that silently failed would leave the aggregated base class in charge and look
// identical from outside.
//
// This is the discriminator. It runs a repeater with a Go layout in a real window and
// asserts three things that only hold if the overrides ran:
//
//   - MeasureOverride was called
//   - ArrangeOverride was called
//   - an element ended up at the position the Go code chose, not where a stack layout
//     would have put it
//
// The third is the one that cannot be faked: the layout places item 3 at a deliberately
// odd offset that no shipped layout would produce.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// goLayoutSubprocessEnv puts the child process into the layout case. Layout needs a real
// window on a UI thread, and a page that kills the process should name itself rather
// than take the suite down — the same reason the theme-resource and gallery cases are
// staged this way.
const goLayoutSubprocessEnv = "WASDK_ACCEPTANCE_GOLAYOUT"

// markerOffset is where the Go layout puts item 3. It is not a multiple of anything the
// shipped layouts would produce, so finding an element here means the Go body ran.
const markerOffset = 137.0

func TestGoVirtualizingLayoutIsCalledByTheFramework(t *testing.T) {
	staging := stageGallerySubprocess(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(staging, "gallery.test.exe"))
	command.Dir = staging
	command.Env = append(os.Environ(), goLayoutSubprocessEnv+"=1")
	output, err := command.CombinedOutput()

	if ctx.Err() != nil {
		t.Fatalf("the Go layout case hung:\n%s", output)
	}
	if err != nil {
		t.Fatalf("the framework did not drive the Go layout: %v\n%s", err, output)
	}
}

// runGoLayoutCase is the child. It returns 0 only if every assertion holds.
func runGoLayoutCase() int {
	failures := make(chan string, 1)

	var layout *pages.GoLayout
	err := app.Run(func(ready *app.Ready) error {
		var buildErr error
		layout, buildErr = pages.NewGoLayout(
			func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect {
				// Item 3 goes to the marker; everything else stacks normally.
				y := float32(index) * 30
				if index == 3 {
					y = markerOffset
				}
				return wrtfoundation.Rect{X: 0, Y: y, Width: 120, Height: 24}
			})
		if buildErr != nil {
			return fmt.Errorf("NewGoLayout: %w", buildErr)
		}

		repeater, err := uixaml.NewItemsRepeater()
		if err != nil {
			return err
		}
		if err := repeater.SetLayout(layout.Layout()); err != nil {
			return fmt.Errorf("SetLayout with the Go layout: %w", err)
		}

		source, err := app.NewStringItemsSource(
			[]string{"a", "b", "c", "d", "e", "f"}, pages.XamlCollectionIIDs())
		if err != nil {
			return err
		}
		if err := repeater.SetItemsSource(source.Inspectable()); err != nil {
			return err
		}

		element, err := repeater.AsUIElement()
		if err != nil {
			return err
		}
		defer element.Release()
		if err := ready.Window.SetContent(element); err != nil {
			return err
		}

		// Checked after layout has run, on the UI thread.
		if err := app.With(repeater.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
					select {
					case failures <- check(layout, repeater):
					default:
					}
					// Exit HERE, on the UI thread. A cross-apartment Exit from a
					// goroutine does not end the message loop — it hangs, which is
					// the same rule app.Run follows when a startup error has to
					// arrange its own shutdown.
					_ = ready.Application.Exit()
				})
			return addErr
		}); err != nil {
			return err
		}

		return nil
	}, app.Options{})

	if err != nil {
		fmt.Fprintln(os.Stderr, "golayout: app.Run:", err)
		return 1
	}
	select {
	case failure := <-failures:
		if failure != "" {
			fmt.Fprintln(os.Stderr, "golayout:", failure)
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "golayout: no result was recorded")
		return 1
	}
	return 0
}

// check returns "" when every assertion holds, or the first failure.
func check(layout *pages.GoLayout, repeater *uixaml.ItemsRepeater) string {
	if layout.MeasureCalls() == 0 {
		return "MeasureOverride was never called: the aggregate is not routing to Go"
	}
	if layout.ArrangeCalls() == 0 {
		return "ArrangeOverride was never called"
	}

	// The geometry assertion. Some element must sit at the marker, which no shipped
	// layout would produce, so this fails if the base class did the arranging.
	//
	// The realized elements are searched rather than indexed, because a repeater's
	// visual children are in realization order rather than item order.
	statics, err := uixaml.VisualTreeHelperStatics()
	if err != nil {
		return "VisualTreeHelperStatics: " + err.Error()
	}
	defer statics.Release()

	element, err := repeater.AsUIElement()
	if err != nil {
		return "AsUIElement: " + err.Error()
	}
	defer element.Release()
	object, err := winrt.QueryInterface[uixaml.IDependencyObject](
		unsafe.Pointer(element), &uixaml.IID_IDependencyObject)
	if err != nil {
		return "the repeater is not a DependencyObject: " + err.Error()
	}
	defer object.Release()

	count, err := statics.GetChildrenCount(object)
	if err != nil {
		return "GetChildrenCount: " + err.Error()
	}
	if count == 0 {
		return "the repeater realized no elements at all"
	}
	for index := int32(0); index < count; index++ {
		child, err := statics.GetChild(object, index)
		if err != nil || child == nil {
			continue
		}
		frame, qiErr := winrt.QueryInterface[uixaml.IFrameworkElement](
			unsafe.Pointer(child), &uixaml.IID_IFrameworkElement)
		child.Release()
		if qiErr != nil {
			continue
		}
		transform, err := offsetOf(frame)
		frame.Release()
		if err == nil && int(transform) == int(markerOffset) {
			return ""
		}
	}
	return fmt.Sprintf(
		"no realized element sits at y=%.0f, so the Go layout did not arrange them "+
			"(%d elements realized, MeasureOverride called %d times)",
		markerOffset, count, layout.MeasureCalls())
}

// offsetOf reports where an element was actually arranged, relative to its parent.
//
// TransformToVisual against the parent is the supported way to ask: the arranged
// position is not a property, it is the result of the layout pass.
func offsetOf(frame *uixaml.IFrameworkElement) (float64, error) {
	element, err := winrt.QueryInterface[uixaml.IUIElement](
		unsafe.Pointer(frame), &uixaml.IID_IUIElement)
	if err != nil {
		return 0, err
	}
	defer element.Release()

	transform, err := element.TransformToVisual(nil)
	if err != nil {
		return 0, err
	}
	defer transform.Release()
	point, err := transform.TransformPoint(wrtfoundation.Point{})
	if err != nil {
		return 0, err
	}
	return float64(point.Y), nil
}
