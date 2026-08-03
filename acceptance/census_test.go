//go:build windows && amd64

package acceptance

// Which controls in this gallery actually do anything?
//
// TestGalleryPagesLayOut proves a page can be built. TestPagesBehaveWhenDriven proves
// eight named behaviours. Neither answers the question this file exists for: across all
// 202 pages, how many controls are live, and how many accept an interaction and change
// nothing? That number is the difference between a gallery and a set of screenshots,
// and it was previously unknown.
//
// It is measured through the SAME automation peers TestPagesBehaveWhenDriven uses, so
// what it reports is what a screen reader — or a person — would find. Driving a peer is
// the control's own code path: the pattern provider raises the control's event, the
// handler runs, and some TextBlock in the tree changes or does not.
//
// # WHY NOT THE UI-AUTOMATION CLIENT IN A VM
//
// An out-of-process UIA client driving the built gallery was tried first and produced
// three confident, WRONG results in a row: a sweep whose selection silently stopped
// advancing and reported the last 34 pages fine, a run that went blind because the
// foreground window changed and reported 172 pages unreachable, and a run that recorded
// findings for pages it never navigated to. Each needed a screenshot to disprove.
//
// In-process peers have none of those failure modes. There is no navigation — the page
// is built directly — no foreground window, no relay, and no chance of attributing one
// page's result to another. It also needs no interactive desktop, so it runs in CI
// beside the other acceptance tests rather than only on a machine someone is watching.
//
// # RUNNING IT
//
//	WASDK_CENSUS=1 go test ./acceptance -run TestPageCensus -timeout 90m
//
// Opt-in because it builds 202 pages in 202 subprocesses. It is a measurement, not a
// gate; the gate it feeds is the per-page Behaviour contract.
//
// # "DEAD" IS A SHORTLIST, NOT A VERDICT
//
// Every observation here is made SYNCHRONOUSLY, on the turn the control was driven, so
// an effect that is animated or deferred cannot be seen however correct it is. Three
// scrolling pages proved this: ScrollView and ScrollPresenter answer StartBringIntoView
// by animating, and a ScrollPresenter settles ZoomTo on a snap point over time. All four
// controls score dead here and all four are demonstrably correct — zz_behaviour_test.go
// asserts them, settling in real time on a DispatcherQueueTimer.
//
// Settling in real time per control is not an option at this scale: 1,500-odd controls
// at ~700ms each is hours, and the census is meant to be a cheap sweep. So the division
// of labour is deliberate — the census produces a SHORTLIST cheaply, and a Behaviour
// scenario adjudicates each entry properly. Anything animated will sit on that shortlist
// permanently, and that is a limitation of the sweep rather than a fault in the page.
//
// Its output, metadata/census.json, is GITIGNORED on purpose: it is a snapshot of one
// run on one machine — control counts move with the Windows App SDK version and with
// what the framework realizes — so committing it would invite reading a stale file as
// a fact about the repository. Regenerate it with the command above.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// censusSubprocessEnv names the page the child should census.
const censusSubprocessEnv = "WASDK_CENSUS_PAGE"

// censusMarker prefixes the child's one line of JSON so it can be found among whatever
// else the framework writes to stdout.
const censusMarker = "CENSUS-JSON: "

// control is one element that offered at least one pattern.
//
// Two separate questions are recorded, because conflating them mis-ranks the work.
// SelfChanged asks whether the CONTROL moved — a CheckBox whose ToggleState flipped
// did its job whatever the page did. Reported asks whether the PAGE said so. A
// control that moved and was not reported is a fidelity question about the page; a
// control that did not move at all is a defect.
type control struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
	// Driven is the pattern that was exercised.
	Driven string `json:"driven,omitempty"`
	// SelfChanged is whether the pattern's own state read back differently
	// afterwards. Invoke has no state to read, so it is always false there.
	SelfChanged bool `json:"selfChanged"`
	// Reported records whether any TextBlock in the page changed afterwards.
	Reported bool `json:"reported"`
	// OpenedPopup records whether the interaction put something in the popup root.
	//
	// The third observable, and without it a whole class of working button scores
	// as dead: "Show the targeted tip", "Open the menu" and a DropDownButton's
	// "Sort by" all do their job by opening a flyout, tip or menu, which is
	// parented to the XamlRoot rather than to the page — so neither the button's
	// own state nor any TextBlock moves.
	OpenedPopup bool `json:"openedPopup"`
	// Failure is why driving it did not happen or did not work.
	Failure string `json:"failure,omitempty"`
}

// pageCensus is one page's answer.
type pageCensus struct {
	Page string `json:"page"`
	// Elements is every element walked, whether or not it had a peer.
	Elements int `json:"elements"`
	// Live is the count of controls offering a drivable pattern.
	Live int `json:"live"`
	// Responsive is how many of those made the PAGE report something.
	Responsive int `json:"responsive"`
	// SelfChanged is how many moved their OWN state — the control worked, whether
	// or not the page mentioned it.
	SelfChanged int `json:"selfChanged"`
	// Opened is how many put something in the popup root.
	Opened int `json:"opened"`
	// Dead is how many accepted the interaction and changed nothing either way.
	// This is the number the remediation is actually chasing.
	Dead     int       `json:"dead"`
	Controls []control `json:"controls,omitempty"`
	Build      string    `json:"buildError,omitempty"`
	Crashed    bool      `json:"crashed,omitempty"`
}

// drivable is every pattern the census exercises, in the order it prefers them.
//
// Order matters where an element offers more than one: a ComboBox is both
// ExpandCollapse and Selection, and expanding it is the interaction a person makes
// first. Invoke comes first because a button that does nothing is the finding this
// census exists to count.
var drivable = []struct {
	name string
	kind uixaml.PatternInterface
	// drive returns "" when the pattern was exercised, or why it was not.
	drive func(peer *uixaml.IAutomationPeer) string
	// read returns the pattern's own state, for comparison either side of drive.
	// Nil where the pattern has no state — Invoke is an action, not a value.
	read func(peer *uixaml.IAutomationPeer) (string, bool)
}{
	{"Invoke", uixaml.PatternInterfaceInvoke, func(peer *uixaml.IAutomationPeer) string {
		provider, err := patternOf[uixaml.IInvokeProvider](
			peer, uixaml.PatternInterfaceInvoke, &uixaml.IID_IInvokeProvider)
		if err != nil {
			return err.Error()
		}
		defer provider.Release()
		if err := provider.Invoke(); err != nil {
			return err.Error()
		}
		return ""
	}, nil},
	{"Toggle", uixaml.PatternInterfaceToggle, func(peer *uixaml.IAutomationPeer) string {
		provider, err := patternOf[uixaml.IToggleProvider](
			peer, uixaml.PatternInterfaceToggle, &uixaml.IID_IToggleProvider)
		if err != nil {
			return err.Error()
		}
		defer provider.Release()
		if err := provider.Toggle(); err != nil {
			return err.Error()
		}
		return ""
	}, func(peer *uixaml.IAutomationPeer) (string, bool) {
		provider, err := patternOf[uixaml.IToggleProvider](
			peer, uixaml.PatternInterfaceToggle, &uixaml.IID_IToggleProvider)
		if err != nil {
			return "", false
		}
		defer provider.Release()
		state, err := provider.ToggleState()
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%d", state), true
	}},
	{"ExpandCollapse", uixaml.PatternInterfaceExpandCollapse, func(peer *uixaml.IAutomationPeer) string {
		provider, err := patternOf[uixaml.IExpandCollapseProvider](
			peer, uixaml.PatternInterfaceExpandCollapse, &uixaml.IID_IExpandCollapseProvider)
		if err != nil {
			return err.Error()
		}
		defer provider.Release()
		if err := provider.Expand(); err != nil {
			return err.Error()
		}
		// Leave the tree as it was found: an open flyout swallows the next
		// element's interaction, which would be recorded as that element failing.
		// The state is read BEFORE this collapse by the caller's second read, so
		// collapsing here would hide the change — it is deliberately not done.
		return ""
	}, func(peer *uixaml.IAutomationPeer) (string, bool) {
		provider, err := patternOf[uixaml.IExpandCollapseProvider](
			peer, uixaml.PatternInterfaceExpandCollapse, &uixaml.IID_IExpandCollapseProvider)
		if err != nil {
			return "", false
		}
		defer provider.Release()
		state, err := provider.ExpandCollapseState()
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%d", state), true
	}},
	{"SelectionItem", uixaml.PatternInterfaceSelectionItem, func(peer *uixaml.IAutomationPeer) string {
		provider, err := patternOf[uixaml.ISelectionItemProvider](
			peer, uixaml.PatternInterfaceSelectionItem, &uixaml.IID_ISelectionItemProvider)
		if err != nil {
			return err.Error()
		}
		defer provider.Release()
		if err := provider.Select(); err != nil {
			return err.Error()
		}
		return ""
	}, func(peer *uixaml.IAutomationPeer) (string, bool) {
		provider, err := patternOf[uixaml.ISelectionItemProvider](
			peer, uixaml.PatternInterfaceSelectionItem, &uixaml.IID_ISelectionItemProvider)
		if err != nil {
			return "", false
		}
		defer provider.Release()
		selected, err := provider.IsSelected()
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%t", selected), true
	}},
	{"RangeValue", uixaml.PatternInterfaceRangeValue, func(peer *uixaml.IAutomationPeer) string {
		provider, err := patternOf[uixaml.IRangeValueProvider](
			peer, uixaml.PatternInterfaceRangeValue, &uixaml.IID_IRangeValueProvider)
		if err != nil {
			return err.Error()
		}
		defer provider.Release()
		// Half way between the bounds, so the value genuinely changes wherever the
		// control started. Setting a constant would score a control already there
		// as unresponsive.
		low, err := provider.Minimum()
		if err != nil {
			return err.Error()
		}
		high, err := provider.Maximum()
		if err != nil {
			return err.Error()
		}
		if err := provider.SetValue(low + (high-low)/2); err != nil {
			return err.Error()
		}
		return ""
	}, func(peer *uixaml.IAutomationPeer) (string, bool) {
		provider, err := patternOf[uixaml.IRangeValueProvider](
			peer, uixaml.PatternInterfaceRangeValue, &uixaml.IID_IRangeValueProvider)
		if err != nil {
			return "", false
		}
		defer provider.Release()
		value, err := provider.Value()
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%g", value), true
	}},
	{"Value", uixaml.PatternInterfaceValue, func(peer *uixaml.IAutomationPeer) string {
		provider, err := patternOf[uixaml.IValueProvider](
			peer, uixaml.PatternInterfaceValue, &uixaml.IID_IValueProvider)
		if err != nil {
			return err.Error()
		}
		defer provider.Release()
		if err := provider.SetValue("census"); err != nil {
			return err.Error()
		}
		return ""
	}, func(peer *uixaml.IAutomationPeer) (string, bool) {
		provider, err := patternOf[uixaml.IValueProvider](
			peer, uixaml.PatternInterfaceValue, &uixaml.IID_IValueProvider)
		if err != nil {
			return "", false
		}
		defer provider.Release()
		value, err := provider.Value()
		if err != nil {
			return "", false
		}
		return value, true
	}},
}

func TestPageCensus(t *testing.T) {
	if os.Getenv("WASDK_CENSUS") == "" {
		t.Skip("set WASDK_CENSUS=1 to run the census; it builds every page in its own subprocess")
	}
	staging := stageGallerySubprocess(t)

	var results []pageCensus
	for _, page := range pages.Buildable() {
		key := page.Key()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		command := exec.CommandContext(ctx, filepath.Join(staging, "gallery.test.exe"))
		command.Dir = staging
		command.Env = append(os.Environ(), censusSubprocessEnv+"="+key)
		output, err := command.CombinedOutput()
		cancel()

		result, decodeErr := decodeCensus(output)
		switch {
		case decodeErr != nil:
			// No line means the child died before reporting — which is itself the
			// most interesting result a page can produce.
			result = pageCensus{Page: key, Crashed: true,
				Build: fmt.Sprintf("no census line (%v); process error: %v", decodeErr, err)}
		case result.Page != key:
			t.Errorf("%s: the child reported page %q", key, result.Page)
			continue
		}
		results = append(results, result)
		t.Logf("%-58s live=%-3d selfChanged=%-3d reported=%-3d dead=%-3d",
			key, result.Live, result.SelfChanged, result.Responsive, result.Dead)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Page < results[j].Page })
	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("encoding the census: %v", err)
	}
	path := filepath.Join("..", "metadata", "census.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	var live, reported, selfChanged, dead, silent, crashed int
	for _, r := range results {
		live += r.Live
		reported += r.Responsive
		selfChanged += r.SelfChanged
		dead += r.Dead
		if r.Crashed {
			crashed++
		}
		// The control worked and the page never said so: a fidelity question
		// about the page, not a broken control.
		if r.SelfChanged > 0 && r.Responsive == 0 {
			silent++
		}
	}
	t.Logf("%d pages, %d live controls: %d moved their own state, %d made the page report, "+
		"%d did nothing at all; %d pages work silently, %d crashed → %s",
		len(results), live, selfChanged, reported, dead, silent, crashed, path)
}

func decodeCensus(output []byte) (pageCensus, error) {
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		payload, found := strings.CutPrefix(strings.TrimSpace(line), censusMarker)
		if !found {
			continue
		}
		var result pageCensus
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			return pageCensus{}, err
		}
		return result, nil
	}
	return pageCensus{}, fmt.Errorf("no %q line in the child's output", strings.TrimSpace(censusMarker))
}

// runPageCensus is the child: build one page, drive everything on it, report one line.
func runPageCensus(key string) int {
	result := pageCensus{Page: key}

	err := app.Run(func(ready *app.Ready) error {
		page, err := pages.Lookup(key)
		if err != nil {
			return err
		}
		if page.Build == nil {
			return fmt.Errorf("%s is unmappable: %s", key, page.Unmappable)
		}
		root, err := page.Build(ready)
		if err != nil {
			result.Build = err.Error()
			return ready.Application.Exit()
		}

		// Census at Loaded, for the same reason the layout suite waits for it: before
		// then the template has not been applied, so a control's peer does not yet
		// offer the patterns its template gives it.
		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				censusTree(ready, root, &result)
				_ = ready.Application.Exit()
			})
		if err != nil {
			return err
		}
		// A page hands back IUIElement; Loaded is on IFrameworkElement, and the two
		// are related by class inheritance rather than a Requires clause, so there is
		// no generated accessor between them.
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
	}, app.Options{
		OnBindingFailed: func(message string) {
			fmt.Fprintln(os.Stderr, "BINDING-FAILED:", message)
		},
		TraceBindings: true,
	})
	if err != nil {
		result.Build = err.Error()
	}

	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, "census: encoding:", marshalErr)
		return 1
	}
	fmt.Println(censusMarker + string(encoded))
	return 0
}

// walkOwned visits the elements the PAGE put there, and stops at the boundary of any
// control's template.
//
// The visual tree is the wrong unit for this question. VisualTreeHelper descends into
// every control's template, so a page built as scrolled(stack(...)) — which is most of
// them — yields the ScrollViewer's own scrollbars as "controls", and a scrollbar that
// will not take a value because its content does not scroll is not a defect in the
// page. Counting those inflated the dead tally with template parts nobody wrote.
//
// The logical tree has no walker in the projection, so it is followed here through the
// three properties the gallery's own helpers build with — Panel.Children (stack, gridOf),
// ContentControl.Content (scrolled, and every ContentControl page host) and Border.Child.
// Reaching a leaf control ends that branch: whatever is inside a Button is its template.
//
// Known limit, stated rather than hidden: items realized from a DataTemplate by an
// ItemsControl are NOT reached, because they hang off the generated container rather
// than off Items. Pages whose subject is a list therefore report their list control and
// not its rows, which is the right unit for "does this page's control work" and the
// wrong one for "does every row work".
func walkOwned(element *uixaml.IUIElement, depth int, visit func(*uixaml.IUIElement)) {
	if element == nil || depth > 32 {
		return
	}
	visit(element)

	// A Panel's children are authored content; recurse.
	if panel, err := winrt.QueryInterface[uixaml.IPanel](
		unsafe.Pointer(element), &uixaml.IID_IPanel); err == nil {
		defer panel.Release()
		if children, err := panel.Children(); err == nil {
			defer children.Release()
			if size, err := children.Size(); err == nil {
				for index := uint32(0); index < size; index++ {
					child, err := children.GetAt(index)
					if err != nil {
						continue
					}
					walkOwned(child, depth+1, visit)
					child.Release()
				}
			}
		}
		return
	}

	if border, err := winrt.QueryInterface[uixaml.IBorder](
		unsafe.Pointer(element), &uixaml.IID_IBorder); err == nil {
		defer border.Release()
		if child, err := border.Child(); err == nil && child != nil {
			walkOwned(child, depth+1, visit)
			child.Release()
		}
		return
	}

	// A ContentControl's Content is authored ONLY when it is an element. A string or a
	// boxed value is the control's own content, not a child, and stops the walk.
	if host, err := winrt.QueryInterface[uixaml.IContentControl](
		unsafe.Pointer(element), &uixaml.IID_IContentControl); err == nil {
		defer host.Release()
		content, err := host.Content()
		if err != nil || content == nil {
			return
		}
		defer content.Release()
		if child, err := winrt.QueryInterface[uixaml.IUIElement](
			unsafe.Pointer(content), &uixaml.IID_IUIElement); err == nil {
			walkOwned(child, depth+1, visit)
			child.Release()
		}
	}
}

// censusTree walks the page once, driving each element's first drivable pattern and
// recording whether the page's own status text changed as a result.
func censusTree(ready *app.Ready, root *uixaml.IUIElement, result *pageCensus) {
	// Collect OWNED references before driving anything.
	//
	// walk hands the callback a borrowed pointer and releases it when the callback
	// returns, so keeping the raw pointer and using it afterwards is a use-after-free
	// — it crashes inside CreatePeerForElement with 0xc0000005, which reads exactly
	// like a broken page and is not one. QueryInterface takes a reference of our own,
	// which is what findByName does for the same reason.
	//
	// Two passes rather than driving inside the walk, because driving a control
	// mutates the tree the walk is in the middle of.
	var elements []*uixaml.IUIElement
	walkOwned(root, 0, func(element *uixaml.IUIElement) {
		result.Elements++
		owned, err := winrt.QueryInterface[uixaml.IUIElement](
			unsafe.Pointer(element), &uixaml.IID_IUIElement)
		if err == nil {
			elements = append(elements, owned)
		}
	})
	defer func() {
		for _, element := range elements {
			element.Release()
		}
	}()

	// Two passes, the second in reverse.
	//
	// A control driven from a state where it has nothing to do looks identical to a
	// broken one. InfoBarPage opens its bar as Informational, so "Informational" and
	// "Reopen" set the state it is already in; AnimatedIconPage starts in Normal, so
	// "Normal" does nothing. All four scored dead and all four work.
	//
	// Driving everything once changes the page, so a second pass reaches each control
	// from a different state. Reverse order because the last control driven is the one
	// least likely to have set the state the first control wants.
	entries := make([]*control, len(elements))
	for pass := range 2 {
		for i := range elements {
			// Reverse on the second pass: the control driven last is the one least
			// likely to have set the state the first control wants.
			index := i
			if pass == 1 {
				index = len(elements) - 1 - i
			}
			if pass == 1 && (entries[index] == nil || answered(entries[index])) {
				continue
			}
			drive(ready, root, elements[index], &entries[index])
		}
	}

	for _, entry := range entries {
		if entry == nil {
			continue
		}
		result.Live++
		// Each observable is counted independently — a control can move its own
		// state AND be reported — while Dead is the absence of all three.
		if entry.SelfChanged {
			result.SelfChanged++
		}
		if entry.Reported {
			result.Responsive++
		}
		if entry.OpenedPopup {
			result.Opened++
		}
		if entry.Failure == "" && !answered(entry) {
			result.Dead++
		}
		result.Controls = append(result.Controls, *entry)
	}
}

// answered reports whether a control has already shown it does something.
func answered(entry *control) bool {
	return entry.SelfChanged || entry.Reported || entry.OpenedPopup || entry.Failure != ""
}

// drive exercises one element's first drivable pattern, recording all three observables.
func drive(ready *app.Ready, root *uixaml.IUIElement, element *uixaml.IUIElement, into **control) {
	peer, err := peerOf(element)
	if err != nil || peer == nil {
		return
	}
	defer peer.Release()

	entry := *into
	if entry == nil {
		entry = &control{}
		if name, err := peer.GetName(); err == nil {
			entry.Name = name
		}
		for _, candidate := range drivable {
			pattern, err := peer.GetPattern(candidate.kind)
			if err != nil || pattern == nil {
				continue
			}
			pattern.Release()
			entry.Patterns = append(entry.Patterns, candidate.name)
		}
		if len(entry.Patterns) == 0 {
			return
		}
		*into = entry
	}

	beforeText := allStatus(root)
	popupsBefore := app.OpenPopupCount(ready)
	for _, candidate := range drivable {
		if candidate.name != entry.Patterns[0] {
			continue
		}
		entry.Driven = candidate.name
		beforeState, readable := "", false
		if candidate.read != nil {
			beforeState, readable = candidate.read(peer)
		}
		entry.Failure = candidate.drive(peer)
		if entry.Failure == "" && readable {
			if afterState, ok := candidate.read(peer); ok && afterState != beforeState {
				entry.SelfChanged = true
			}
		}
		break
	}
	if entry.Failure == "" && allStatus(root) != beforeText {
		entry.Reported = true
	}
	if entry.Failure == "" && app.OpenPopupCount(ready) != popupsBefore {
		entry.OpenedPopup = true
	}
	// Close whatever opened before moving on: a flyout left up is modal to the next
	// interaction, which would then be recorded as that control failing.
	app.CloseOpenPopups(ready)
}
