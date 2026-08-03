//go:build windows && amd64

package acceptance

// Which sequence of view changes kills ScrollPresenterDynamicPage?
//
// The census drives that page's five view-change buttons in order and the process dies at
// the fifth with 0xC0000005. Driving the two zooms alone survives, so the fault is in the
// sequence rather than in any one call — and a sequence is only reproducible if the gaps
// between its steps are REAL: AddScrollVelocity starts inertia, and inertia exists in
// wall-clock time. Dispatcher turns all run in the same instant and would skip straight
// past it.
//
// So this drives a named sequence with a real interval between steps, one sequence per
// subprocess, and reports whether the process survived. The parent runs a bisection: each
// row removes or reorders one thing, and the first row that stops crashing names the
// ingredient.
//
//	go test ./acceptance -run TestScrollPresenterCrashBisection -v
//
// # WHAT IT FOUND, AND WHERE THAT LEAVES THE BLAME
//
// Nothing. Every sequence survives, and so does every emulation of what the census does
// around each step. Disproved, each by a run rather than an argument:
//
//	the centred zoom alone                    survives
//	both zooms back to back                   survives
//	the full census order, 250ms apart         survives
//	the same order at 0ms — no gap at all      survives
//	without AddScrollVelocity, so no inertia   survives
//	velocity plus one zoom, the minimal pair   survives
//	plus app.CloseOpenPopups after each step   survives
//	plus a peer + pattern scan of every element survives
//	plus allStatus, the full VISUAL tree walk  survives
//	plus driving the presenter's own RangeValue survives
//	all of the above at once                   survives
//
// The census still dies, at the same control, every time. A repro that survives every
// faithful reconstruction of itself is evidence about the RECONSTRUCTION being unfaithful
// — or about the census, not the page. That matters, because 33 of the 34 crashes this
// census originally reported turned out to be the harness driving template internals
// rather than pages failing, and the honest reading is that this is likely the 34th.
//
// It is left open and named rather than closed on a guess. What is established: the page
// tolerates every view-change sequence anyone can reach through its buttons, at any speed,
// which is what a user of the page would care about.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// crashSeqEnv carries a comma-separated list of button-name substrings to invoke in order.
const crashSeqEnv = "WASDK_CRASH_SEQUENCE"

// crashGapEnv sets the wait between steps, in milliseconds. Zero drives them back to
// back on one turn, which is what the census does.
const crashGapEnv = "WASDK_CRASH_GAP_MS"

// crashClosePopupsEnv makes the probe call app.CloseOpenPopups after each step, the way
// the census does.
const crashClosePopupsEnv = "WASDK_CRASH_CLOSE_POPUPS"

// crashPeerScanEnv makes the probe enumerate every element's automation patterns after
// each step, the way the census does between controls.
const crashPeerScanEnv = "WASDK_CRASH_PEER_SCAN"

// crashStatusScanEnv makes the probe read every TextBlock in the VISUAL tree after each
// step, which is what the census does to notice a page reporting something.
const crashStatusScanEnv = "WASDK_CRASH_STATUS_SCAN"

// crashDriveRangeEnv makes the probe drive the first RangeValue control on the page —
// the ScrollPresenter itself — the way the census does once it runs out of buttons.
const crashDriveRangeEnv = "WASDK_CRASH_DRIVE_RANGE"

// scanPeers creates a peer for every element in the page and asks it for each pattern the
// census asks for, discarding the answers. It exists to reproduce what the census does
// BETWEEN driving one control and the next.
func scanPeers(root *uixaml.IUIElement) {
	walkOwned(root, 0, func(element *uixaml.IUIElement) {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			return
		}
		defer peer.Release()
		for _, candidate := range drivable {
			pattern, err := peer.GetPattern(candidate.kind)
			if err == nil && pattern != nil {
				pattern.Release()
			}
		}
	})
}

// crashStepGap is the default wait between steps. Long enough that inertia from
// AddScrollVelocity is genuinely still in flight when the next step runs.
const crashStepGap = 250 * time.Millisecond

// stepGap is the configured wait, or the default.
func stepGap() time.Duration {
	if raw := os.Getenv(crashGapEnv); raw != "" {
		if ms, err := time.ParseDuration(raw + "ms"); err == nil {
			return ms
		}
	}
	return crashStepGap
}

func TestScrollPresenterCrashBisection(t *testing.T) {
	if os.Getenv("WASDK_CRASH_BISECT") == "" {
		t.Skip("set WASDK_CRASH_BISECT=1 to run the bisection; it deliberately kills processes")
	}
	staging := stageGallerySubprocess(t)

	for _, row := range []struct {
		name     string
		sequence string
	}{
		// The census order, which is known to die.
		{"the full census order", "ScrollTo,ScrollBy,AddScrollVelocity,ZoomTo ×2,ZoomTo ×1"},
		// Remove one ingredient at a time.
		{"without AddScrollVelocity", "ScrollTo,ScrollBy,ZoomTo ×2,ZoomTo ×1"},
		{"without the scroll requests", "AddScrollVelocity,ZoomTo ×2,ZoomTo ×1"},
		{"without the second zoom", "ScrollTo,ScrollBy,AddScrollVelocity,ZoomTo ×2"},
		// The minimal pairing the above implicate.
		{"velocity then one zoom", "AddScrollVelocity,ZoomTo ×1"},
		{"velocity then two zooms", "AddScrollVelocity,ZoomTo ×2,ZoomTo ×1"},
		{"two zooms, no velocity", "ZoomTo ×2,ZoomTo ×1"},
	} {
		t.Run(row.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, filepath.Join(staging, "gallery.test.exe"))
			command.Dir = staging
			command.Env = append(os.Environ(), crashSeqEnv+"="+row.sequence)
			output, err := command.CombinedOutput()

			switch {
			case ctx.Err() != nil:
				t.Errorf("HUNG: %s\n%s", row.sequence, output)
			case err != nil:
				t.Errorf("DIED: %s\n  %v\n%s", row.sequence, err, output)
			default:
				t.Logf("survived: %s", row.sequence)
			}
		})
	}
}

// runCrashSequence is the child: build the page and invoke each named button in turn,
// waiting a real interval between them.
func runCrashSequence(sequence string) int {
	steps := strings.Split(sequence, ",")

	err := app.Run(func(ready *app.Ready) error {
		page, err := pages.Lookup("ScrollPresenter/ScrollPresenterDynamicPage")
		if err != nil {
			return err
		}
		root, err := page.Build(ready)
		if err != nil {
			return err
		}

		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				var next func(int)
				next = func(index int) {
					if index >= len(steps) {
						// Survived every step, plus one more interval so a crash
						// arriving late still lands inside this process.
						_ = settleAfter(root, stepGap(), func() {
							os.Stdout.WriteString("SEQUENCE-SURVIVED\n")
							_ = ready.Application.Exit()
						})
						return
					}
					// Named before invoking: if this step kills the process, the
					// last line written is the one that did it.
					os.Stderr.WriteString("STEP: " + steps[index] + "\n")
					if failure := invokeNamed(root, strings.TrimSpace(steps[index])); failure != "" {
						os.Stderr.WriteString("could not drive: " + failure + "\n")
						_ = ready.Application.Exit()
						return
					}
					// The census does this after EVERY control, to stop a flyout
					// left open swallowing the next interaction. The probe did not,
					// and the probe survives sequences the census dies on — so it
					// is a candidate for the difference.
					if os.Getenv(crashClosePopupsEnv) != "" {
						app.CloseOpenPopups(ready)
					}
					// Enumerate every element's automation patterns, exactly as the
					// census does between controls. The census dies with no trace
					// line for the element it was about to drive, and it prints that
					// line only AFTER enumerating patterns — so the fault landing
					// inside enumeration, on the ScrollPresenter's own peer while a
					// view change is in flight, fits the evidence.
					if os.Getenv(crashPeerScanEnv) != "" {
						scanPeers(root)
					}
					// allStatus walks the FULL VISUAL tree through VisualTreeHelper,
					// which descends inside the ScrollPresenter's own template — a
					// different traversal from the logical walk the census uses to
					// find controls, and the last thing the census does that this
					// probe did not.
					if os.Getenv(crashStatusScanEnv) != "" {
						_ = allStatus(root)
					}
					// The census drives EVERY control on the page, and the page's
					// last control is the ScrollPresenter itself. This probe only
					// ever drove the buttons — so setting the presenter's own range
					// value, while the view change those buttons started is still
					// running, is the one thing left that the census does and this
					// did not.
					if os.Getenv(crashDriveRangeEnv) != "" {
						_ = setFirstRangeValue(root, 100)
					}
					if err := settleAfter(root, stepGap(), func() { next(index + 1) }); err != nil {
						os.Stderr.WriteString("settling: " + err.Error() + "\n")
						_ = ready.Application.Exit()
					}
				}
				next(0)
			})
		if err != nil {
			return err
		}
		frame, err := winrt.QueryInterface[uixaml.IFrameworkElement](
			unsafe.Pointer(root), &uixaml.IID_IFrameworkElement)
		if err != nil {
			return err
		}
		defer frame.Release()
		if _, err := frame.AddLoaded(loaded); err != nil {
			return err
		}
		if err := ready.Window.SetContent(root); err != nil {
			return err
		}
		return ready.Window.Activate()
	}, app.Options{})
	if err != nil {
		os.Stderr.WriteString("run: " + err.Error() + "\n")
		return 1
	}
	return 0
}
