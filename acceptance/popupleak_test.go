//go:build windows && amd64

package acceptance

// Does swapping content out close what that content opened?
//
// A popup is hosted in the XamlRoot's POPUP ROOT, not in the element tree of whatever
// opened it. Dropping a subtree therefore does not close what the subtree opened: the
// popup has a different parent and does not know its opener is gone.
//
// It was found in the gallery. AnnotatedScrollBar's detail label — the "At 0" tooltip
// beside the thumb — stayed on screen over every page visited afterwards, and a fresh one
// appeared on each return to the page.
//
// THIS TEST DOES NOT REPRODUCE THAT PAGE, and the first version's attempt is why. It
// built AnnotatedScrollBarSummaryPage, waited for Loaded, and measured ZERO popups open:
// the detail label appears on hover or drag, so reproducing it needs pointer input a
// headless run does not have. Asserting on it anyway would have been a test that passes
// because nothing happened — which is worse than no test, because it reads like coverage.
//
// So this tests the contract the fix implements, which is the part that generalizes: a
// popup opened under a subtree must not survive that subtree being swapped out. A Flyout
// is opened explicitly, needing no input, and the assertion is against the popup ROOT
// rather than any one control — so it holds for a detail label, a flyout, a tooltip or a
// dialog alike.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

const popupLeakSubprocessEnv = "WASDK_ACCEPTANCE_POPUP_LEAK"

func TestPagesDoNotLeavePopupsBehind(t *testing.T) {
	staging := stageGallerySubprocess(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(staging, "gallery.test.exe"))
	command.Dir = staging
	command.Env = append(os.Environ(), popupLeakSubprocessEnv+"=1")
	output, err := command.CombinedOutput()

	if ctx.Err() != nil {
		t.Fatalf("the popup-leak case hung:\n%s", output)
	}
	if err != nil {
		t.Fatalf("a popup survived its opener being swapped out: %v\n%s", err, output)
	}
}

// runPopupLeakCase opens a flyout, swaps the content out the way the shell does, and
// checks the popup is gone afterwards.
func runPopupLeakCase() int {
	failures := make(chan string, 1)

	runErr := app.Run(func(ready *app.Ready) error {
		host, err := uixaml.NewContentControl()
		if err != nil {
			return err
		}
		hostElement, err := host.AsUIElement()
		if err != nil {
			return err
		}
		defer hostElement.Release()
		if err := ready.Window.SetContent(hostElement); err != nil {
			return err
		}

		// A button with a flyout, standing in for a page that opens one.
		opener, err := uixaml.NewButton()
		if err != nil {
			return err
		}
		if err := app.SetContent(opener.AsContentControl, "opener"); err != nil {
			return err
		}
		flyout, err := uixaml.NewFlyout()
		if err != nil {
			return err
		}
		body, err := uixaml.NewTextBlock()
		if err != nil {
			return err
		}
		defer body.Release()
		if err := body.SetText("stand-in for a detail label"); err != nil {
			return err
		}
		if err := app.With(body.AsUIElement, func(element *uixaml.IUIElement) error {
			return flyout.SetContent(element)
		}); err != nil {
			return err
		}

		openerElement, err := opener.AsUIElement()
		if err != nil {
			return err
		}
		defer openerElement.Release()
		if err := app.With(host.AsContentControl, func(content *uixaml.IContentControl) error {
			return content.SetContent(&openerElement.IInspectable)
		}); err != nil {
			return err
		}

		// Shown once the tree is live: ShowAt before then has nothing to attach to.
		return app.With(opener.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
					failures <- checkPopupTeardown(ready, host, flyout, frame)
					_ = ready.Application.Exit()
				})
			return addErr
		})
	}, app.Options{})

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "popupleak: app.Run:", runErr)
		return 1
	}
	select {
	case failure := <-failures:
		if failure != "" {
			fmt.Fprintln(os.Stderr, "popupleak:", failure)
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "popupleak: no result was recorded")
		return 1
	}
	return 0
}

// checkPopupTeardown returns "" when the contract holds, or the first failure.
func checkPopupTeardown(ready *app.Ready, host *uixaml.ContentControl,
	flyout *uixaml.Flyout, frame *uixaml.IFrameworkElement,
) string {
	if err := app.With(flyout.AsFlyoutBase, func(base *uixaml.IFlyoutBase) error {
		return base.ShowAt(frame)
	}); err != nil {
		return "opening the flyout: " + err.Error()
	}

	// Counted BEFORE anything is closed. A zero here means the flyout never opened, so
	// the rest of the test would pass for the wrong reason — that is a failure, not a
	// quiet success.
	opened := app.OpenPopupCount(ready)

	_ = app.With(host.AsContentControl, func(content *uixaml.IContentControl) error {
		return content.SetContent(nil)
	})
	leaked := app.OpenPopupCount(ready)

	app.CloseOpenPopups(ready)
	remaining := app.OpenPopupCount(ready)

	switch {
	case opened == 0:
		return "the flyout did not open, so nothing was tested"
	case leaked == 0:
		return "swapping the content out closed the popup by itself, so the leak this " +
			"guards against no longer reproduces — the test needs rewriting rather than " +
			"deleting, since the shell still has to close popups it cannot see"
	case remaining != 0:
		return fmt.Sprintf("%d of %d popup(s) survived CloseOpenPopups", remaining, leaked)
	}
	return ""
}
