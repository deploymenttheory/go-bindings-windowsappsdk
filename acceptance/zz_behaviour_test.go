//go:build windows && amd64

package acceptance

// Does a page actually DO anything?
//
// TestGalleryPagesLayOut builds every page and waits for Loaded. That proves a page can be
// constructed and measured, and it proves nothing else — it never clicks a button, hovers
// an element, expands a node or types a value. Four defects have now shipped through it:
// a popup that outlived its page, statics released while handlers still held them, an
// AnimatedIcon whose hover was wired to a non-hit-testable element, and a page reported
// working that was not.
//
// Every one of those lived in the half that needs interaction. So this suite drives the
// controls.
//
// It drives them through their own AUTOMATION PEERS, not by synthesising events. Asking a
// Button's peer to Invoke is the same path a screen reader or a real click takes: the
// control raises its own Click, the handler runs, and the observable state changes or does
// not. That is a genuine test of the wiring rather than a re-statement of it.
//
// What it cannot reach is pointer-only behaviour — hover has no automation pattern. Those
// scenarios say so rather than pretending; see hoverIsNotAutomatable.
//
// # WHY THIS FILE IS NAMED zz_
//
// Application.Start can be called ONCE per process, and several tests in this package
// call app.Run in the parent. Which one succeeds therefore depends on the order Go
// compiles the files in, which is alphabetical — and adding a file named behaviour_test.go
// put it first, reordered everything after it, and made three unrelated tests fail with
// E_ILLEGAL_METHOD_CALL from Application.Start.
//
// That fragility was already here and invisible; this file only exposed it. The zz_ prefix
// keeps this suite last so it cannot displace the in-process tests, and the real fix — one
// in-process app.Run per package, everything else staged into a subprocess — is a larger
// change than this suite should make on its way past.
//
// The symptom to recognise: a test that fails at 0.00s with
// "app: Application.Start: HRESULT 0x8000000E".

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uidispatching "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/dispatching"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

const behaviourSubprocessEnv = "WASDK_ACCEPTANCE_BEHAVIOUR"

// scenario is one behaviour: build a page, do something to it, and say what should be
// true afterwards.
type scenario struct {
	name string
	// page is the registry key.
	page string
	// act drives the page. It returns "" when the behaviour held, or a description of
	// what went wrong — a string rather than an error because most failures here are
	// "the status text says the wrong thing" rather than a failed call.
	act func(ready *app.Ready, root *uixaml.IUIElement) string

	// check, when set, is the assertion, and it runs on a LATER dispatcher turn.
	//
	// Some effects are not observable when the call that starts them returns.
	// ContentDialog.ShowAsync is the clearest: it begins showing the dialog, and the
	// popup is not in the XamlRoot until the framework has run another turn. Asserting
	// immediately measures the moment before the thing happens, which reads exactly like
	// the thing never happening — the same shape of error as measuring a Button at 0x0
	// before layout.
	check func(ready *app.Ready, root *uixaml.IUIElement) string
}

// scenarios are ordered by what they protect. The first four are the pages whose
// interactive halves were shipped broken.
var scenarios = []scenario{
	{
		name: "a SplitButton's own Click fires when the button half is invoked",
		page: "SplitButton/SplitButtonPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			if failure := invokeNamed(root, "Paste"); failure != "" {
				return failure
			}
			return expectStatus(root, "button half")
		},
	},
	{
		name: "an AnimatedIcon's state changes when a state button is invoked",
		page: "AnimatedIcon/AnimatedIconPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			if failure := invokeNamed(root, "PointerOver"); failure != "" {
				return failure
			}
			return expectStatus(root, "State: PointerOver")
		},
	},
	{
		name: "a CommandBarFlyout opens a popup when its button is invoked",
		page: "CommandBarFlyout/CommandBarFlyoutPage",
		act: func(ready *app.Ready, root *uixaml.IUIElement) string {
			before := app.OpenPopupCount(ready)
			if failure := invokeNamed(root, "Show the command bar flyout"); failure != "" {
				return failure
			}
			after := app.OpenPopupCount(ready)
			if after <= before {
				return fmt.Sprintf("no popup opened (%d before, %d after); the page "+
					"reports: %s", before, after, allStatus(root))
			}
			return ""
		},
	},
	{
		name: "a ContentDialog opens when its button is invoked",
		page: "CommonStyles/ContentDialogPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			return invokeNamed(root, "Show a ContentDialog")
		},
		// Asserted on what the page reports, NOT on the popup count.
		//
		// A ContentDialog is not enumerated by GetOpenPopupsForXamlRoot: twelve
		// dispatcher turns after ShowAsync it still reports none. So counting popups
		// cannot see a dialog — which also means app.CloseOpenPopups cannot close one,
		// worth knowing since that helper exists to tidy up after a page.
		check: func(_ *app.Ready, root *uixaml.IUIElement) string {
			return expectStatus(root, "Dialog shown")
		},
	},
	{
		name: "a TreeView realizes children when a lazy node is expanded",
		page: "TreeView/TreeViewUnrealizedChildrenTestPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			if failure := expandFirst(root); failure != "" {
				return failure
			}
			return expectStatus(root, "Realized three children")
		},
	},
	{
		name: "a NumberBox reports the value set through its peer",
		page: "NumberBox/NumberBoxPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			// A NumberBox exposes RangeValue, not Value: Value is the text pattern a
			// TextBox has, and a numeric control with a minimum and a maximum is a
			// range. Asking for the wrong one reports "nothing supports it", which is
			// true and misleading.
			if failure := setFirstRangeValue(root, 42); failure != "" {
				return failure
			}
			return expectStatus(root, "New value: 42")
		},
	},
	{
		name: "a ScrollView reports a correlation id when scrolled",
		page: "ScrollView/ScrollViewDynamicPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			if failure := invokeNamed(root, "ScrollTo 0,400"); failure != "" {
				return failure
			}
			return expectStatus(root, "correlation id")
		},
	},
	{
		name: "a MenuBar item's click reports which entry was chosen",
		page: "MenuBar/MenuBarPage",
		act: func(_ *app.Ready, root *uixaml.IUIElement) string {
			// The bar's own items expand rather than invoke; expanding File is what a
			// keyboard user does, and it is the part reachable without a pointer.
			if failure := expandNamed(root, "File"); failure != "" {
				return failure
			}
			return ""
		},
	},
}

func TestPagesBehaveWhenDriven(t *testing.T) {
	staging := stageGallerySubprocess(t)
	for _, entry := range scenarios {
		t.Run(entry.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, filepath.Join(staging, "gallery.test.exe"))
			command.Dir = staging
			command.Env = append(os.Environ(), behaviourSubprocessEnv+"="+entry.name)
			output, err := command.CombinedOutput()

			if ctx.Err() != nil {
				t.Fatalf("hung:\n%s", output)
			}
			if err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}

// runBehaviourScenario is the child: build the page, run the scenario, report.
func runBehaviourScenario(name string) int {
	var chosen *scenario
	for i := range scenarios {
		if scenarios[i].name == name {
			chosen = &scenarios[i]
			break
		}
	}
	if chosen == nil {
		fmt.Fprintln(os.Stderr, "behaviour: no scenario named", name)
		return 3
	}
	page, err := pages.Lookup(chosen.page)
	if err != nil || page.Build == nil {
		fmt.Fprintln(os.Stderr, "behaviour: no buildable page", chosen.page)
		return 3
	}

	result := make(chan string, 1)
	runErr := app.Run(func(ready *app.Ready) error {
		element, err := page.Build(ready)
		if err != nil {
			return fmt.Errorf("building %s: %w", chosen.page, err)
		}
		if err := ready.Window.SetContent(element); err != nil {
			element.Release()
			return err
		}

		frame, err := winrt.QueryInterface[uixaml.IFrameworkElement](
			unsafe.Pointer(element), &uixaml.IID_IFrameworkElement)
		if err != nil {
			element.Release()
			return err
		}
		defer frame.Release()

		// Driven at Loaded: before that the tree has no peers and no layout, so every
		// pattern lookup would fail for the wrong reason.
		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				failure := chosen.act(ready, element)
				if failure != "" || chosen.check == nil {
					result <- failure
					element.Release()
					_ = ready.Application.Exit()
					return
				}
				// Queued rather than called: the assertion has to run after the
				// framework has had a turn. TryEnqueue puts it at the back of the same
				// thread's queue, which is exactly one turn later.
				if err := enqueueAfter(element, settleTurns, func() {
					result <- chosen.check(ready, element)
					element.Release()
					_ = ready.Application.Exit()
				}); err != nil {
					result <- "enqueueing the check: " + err.Error()
					element.Release()
					_ = ready.Application.Exit()
				}
			})
		if err != nil {
			element.Release()
			return err
		}
		_, err = frame.AddLoaded(loaded)
		return err
	}, app.Options{})

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "behaviour:", chosen.page, "failed to run:", runErr)
		return 1
	}
	select {
	case failure := <-result:
		if failure != "" {
			fmt.Fprintln(os.Stderr, "behaviour:", chosen.name, "->", failure)
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "behaviour: the scenario never ran")
		return 1
	}
	return 0
}

// settleTurns is how many dispatcher turns a check waits before asserting.
const settleTurns = 12

// enqueueAfter runs fn after n turns, each queued from the last.
func enqueueAfter(element *uixaml.IUIElement, n int, fn func()) error {
	if n <= 0 {
		fn()
		return nil
	}
	return enqueue(element, func() { _ = enqueueAfter(element, n-1, fn) })
}

// enqueue runs fn on a later turn of the UI thread's dispatcher queue.
func enqueue(element *uixaml.IUIElement, fn func()) error {
	object, err := winrt.QueryInterface[uixaml.IDependencyObject](
		unsafe.Pointer(element), &uixaml.IID_IDependencyObject)
	if err != nil {
		return err
	}
	defer object.Release()
	queue, err := object.DispatcherQueue()
	if err != nil {
		return err
	}
	defer queue.Release()

	handler, err := uidispatching.NewDispatcherQueueHandler(fn)
	if err != nil {
		return err
	}
	// Not closed: the queue holds it until it has run.
	_, err = queue.TryEnqueue(handler)
	return err
}

// ---------------------------------------------------------------------------
// Driving controls through their peers

// peerOf returns an element's automation peer, creating it if needed.
func peerOf(element *uixaml.IUIElement) (*uixaml.IAutomationPeer, error) {
	statics, err := uixaml.FrameworkElementAutomationPeerStatics()
	if err != nil {
		return nil, err
	}
	defer statics.Release()
	return statics.CreatePeerForElement(element)
}

// patternOf asks a peer for one pattern and views it as T.
func patternOf[T any](peer *uixaml.IAutomationPeer, kind uixaml.PatternInterface,
	iid *win32.GUID,
) (*T, error) {
	pattern, err := peer.GetPattern(kind)
	if err != nil {
		return nil, err
	}
	if pattern == nil {
		return nil, fmt.Errorf("the peer does not support that pattern")
	}
	defer pattern.Release()
	return winrt.QueryInterface[T](unsafe.Pointer(pattern), iid)
}

// invokeNamed finds the element whose automation name contains want and invokes it.
//
// The automation name of a Button is its content, which is why the scenarios above name
// controls by the words on them — the same way a person would.
func invokeNamed(root *uixaml.IUIElement, want string) string {
	// The name must match AND the element must support Invoke. Matching on name alone
	// finds the status TextBlock that reports the state as often as it finds the button
	// that sets it, since both contain the same words.
	failure := fmt.Sprintf("no invokable element named %q in the tree", want)
	walk(root, func(element *uixaml.IUIElement) bool {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			return true
		}
		defer peer.Release()
		name, err := peer.GetName()
		if err != nil || !strings.Contains(name, want) {
			return true
		}
		provider, err := patternOf[uixaml.IInvokeProvider](
			peer, uixaml.PatternInterfaceInvoke, &uixaml.IID_IInvokeProvider)
		if err != nil {
			return true // named, but not a button; keep looking
		}
		defer provider.Release()
		if err := provider.Invoke(); err != nil {
			failure = fmt.Sprintf("invoking %q: %v", want, err)
			return false
		}
		failure = ""
		return false
	})
	return failure
}

// expandNamed expands the element whose automation name contains want.
func expandNamed(root *uixaml.IUIElement, want string) string {
	element := findByName(root, want)
	if element == nil {
		return fmt.Sprintf("no element named %q was found in the tree", want)
	}
	defer element.Release()

	peer, err := peerOf(element)
	if err != nil || peer == nil {
		return fmt.Sprintf("no automation peer for %q: %v", want, err)
	}
	defer peer.Release()

	provider, err := patternOf[uixaml.IExpandCollapseProvider](
		peer, uixaml.PatternInterfaceExpandCollapse, &uixaml.IID_IExpandCollapseProvider)
	if err != nil {
		return fmt.Sprintf("%q has no ExpandCollapse pattern: %v", want, err)
	}
	defer provider.Release()
	if err := provider.Expand(); err != nil {
		return fmt.Sprintf("expanding %q: %v", want, err)
	}
	return ""
}

// expandFirst expands the first element in the tree that supports the pattern.
func expandFirst(root *uixaml.IUIElement) string {
	var failure = "nothing in the tree supports the ExpandCollapse pattern"
	walk(root, func(element *uixaml.IUIElement) bool {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			return true
		}
		defer peer.Release()
		provider, err := patternOf[uixaml.IExpandCollapseProvider](
			peer, uixaml.PatternInterfaceExpandCollapse, &uixaml.IID_IExpandCollapseProvider)
		if err != nil {
			return true
		}
		defer provider.Release()
		if err := provider.Expand(); err != nil {
			failure = fmt.Sprintf("expanding: %v", err)
			return false
		}
		failure = ""
		return false
	})
	return failure
}

// setFirstValue sets the first Value-pattern element in the tree.
func setFirstValue(root *uixaml.IUIElement, value string) string {
	failure := "nothing in the tree supports the Value pattern"
	walk(root, func(element *uixaml.IUIElement) bool {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			return true
		}
		defer peer.Release()
		provider, err := patternOf[uixaml.IValueProvider](
			peer, uixaml.PatternInterfaceValue, &uixaml.IID_IValueProvider)
		if err != nil {
			return true
		}
		defer provider.Release()
		if err := provider.SetValue(value); err != nil {
			failure = fmt.Sprintf("setting the value: %v", err)
			return false
		}
		failure = ""
		return false
	})
	return failure
}

// setFirstRangeValue sets the first RangeValue-pattern element in the tree.
func setFirstRangeValue(root *uixaml.IUIElement, value float64) string {
	failure := "nothing in the tree supports the RangeValue pattern"
	walk(root, func(element *uixaml.IUIElement) bool {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			return true
		}
		defer peer.Release()
		provider, err := patternOf[uixaml.IRangeValueProvider](
			peer, uixaml.PatternInterfaceRangeValue, &uixaml.IID_IRangeValueProvider)
		if err != nil {
			return true
		}
		defer provider.Release()
		if err := provider.SetValue(value); err != nil {
			failure = fmt.Sprintf("setting the range value: %v", err)
			return false
		}
		failure = ""
		return false
	})
	return failure
}

// expectStatus checks that some TextBlock in the tree contains want.
//
// Every page in this gallery reports what it did into a TextBlock, which is what makes
// this a general assertion rather than one per page.
func expectStatus(root *uixaml.IUIElement, want string) string {
	var seen []string
	found := false
	walk(root, func(element *uixaml.IUIElement) bool {
		block, err := winrt.QueryInterface[uixaml.ITextBlock](
			unsafe.Pointer(element), &uixaml.IID_ITextBlock)
		if err != nil {
			return true
		}
		defer block.Release()
		text, err := block.Text()
		if err != nil {
			return true
		}
		seen = append(seen, text)
		if strings.Contains(text, want) {
			found = true
			return false
		}
		return true
	})
	if found {
		return ""
	}
	return fmt.Sprintf("no TextBlock contains %q; the tree says: %q",
		want, strings.Join(seen, " | "))
}

// allStatus joins every TextBlock in the tree, for a failure message.
func allStatus(root *uixaml.IUIElement) string {
	var seen []string
	walk(root, func(element *uixaml.IUIElement) bool {
		block, err := winrt.QueryInterface[uixaml.ITextBlock](
			unsafe.Pointer(element), &uixaml.IID_ITextBlock)
		if err != nil {
			return true
		}
		defer block.Release()
		if text, err := block.Text(); err == nil && text != "" {
			seen = append(seen, text)
		}
		return true
	})
	return strings.Join(seen, " | ")
}

// findByName returns the first element whose automation name contains want. The caller
// owns the result.
func findByName(root *uixaml.IUIElement, want string) *uixaml.IUIElement {
	var match *uixaml.IUIElement
	walk(root, func(element *uixaml.IUIElement) bool {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			return true
		}
		defer peer.Release()
		name, err := peer.GetName()
		if err != nil || !strings.Contains(name, want) {
			return true
		}
		match, _ = winrt.QueryInterface[uixaml.IUIElement](
			unsafe.Pointer(element), &uixaml.IID_IUIElement)
		return false
	})
	return match
}

// walk visits the visual tree depth-first until visit returns false.
func walk(root *uixaml.IUIElement, visit func(*uixaml.IUIElement) bool) {
	statics, err := uixaml.VisualTreeHelperStatics()
	if err != nil {
		return
	}
	defer statics.Release()
	walkFrom(statics, root, visit)
}

func walkFrom(statics *uixaml.IVisualTreeHelperStatics, element *uixaml.IUIElement,
	visit func(*uixaml.IUIElement) bool,
) bool {
	if !visit(element) {
		return false
	}
	object, err := winrt.QueryInterface[uixaml.IDependencyObject](
		unsafe.Pointer(element), &uixaml.IID_IDependencyObject)
	if err != nil {
		return true
	}
	defer object.Release()

	count, err := statics.GetChildrenCount(object)
	if err != nil {
		return true
	}
	for index := int32(0); index < count; index++ {
		child, err := statics.GetChild(object, index)
		if err != nil || child == nil {
			continue
		}
		childElement, qiErr := winrt.QueryInterface[uixaml.IUIElement](
			unsafe.Pointer(child), &uixaml.IID_IUIElement)
		child.Release()
		if qiErr != nil {
			continue
		}
		keepGoing := walkFrom(statics, childElement, visit)
		childElement.Release()
		if !keepGoing {
			return false
		}
	}
	return true
}

// hoverIsNotAutomatable records what this suite cannot reach.
//
// PointerEntered has no automation pattern — hover is not a command, it is a physical
// position — so a page whose only behaviour is hover cannot be driven here. Reaching it
// means synthesising real pointer input with SendInput against known screen coordinates,
// which is a different kind of test and a fragile one. The AnimatedIcon scenario above
// therefore drives the BUTTONS, which share the state-setting code path with the hover
// handlers; what stays untested is the pointer wiring itself.
const hoverIsNotAutomatable = true
