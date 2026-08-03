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
//	go test ./acceptance -run TestPageCensus -timeout 60m       (writes metadata/census.json)
//
// Opt-in because it builds 202 pages in 202 subprocesses. It is a measurement, not a
// gate; the gate it feeds is the per-page Behaviour contract.

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
type control struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
	// Driven is the pattern that was exercised.
	Driven string `json:"driven,omitempty"`
	// Responded records whether any TextBlock in the page changed afterwards.
	Responded bool `json:"responded"`
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
	// Responsive is how many of those changed something when driven.
	Responsive int       `json:"responsive"`
	Controls   []control `json:"controls,omitempty"`
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
	}},
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
		_ = provider.Collapse()
		return ""
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
		t.Logf("%-62s elements=%-4d live=%-3d responsive=%-3d", key, result.Elements, result.Live, result.Responsive)
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

	var live, responsive, inert, crashed int
	for _, r := range results {
		live += r.Live
		responsive += r.Responsive
		if r.Crashed {
			crashed++
		}
		if r.Live > 0 && r.Responsive == 0 {
			inert++
		}
	}
	t.Logf("%d pages: %d live controls, %d responded, %d pages wholly inert, %d crashed → %s",
		len(results), live, responsive, inert, crashed, path)
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
				censusTree(root, &result)
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

// censusTree walks the page once, driving each element's first drivable pattern and
// recording whether the page's own status text changed as a result.
func censusTree(root *uixaml.IUIElement, result *pageCensus) {
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
	walk(root, func(element *uixaml.IUIElement) bool {
		result.Elements++
		owned, err := winrt.QueryInterface[uixaml.IUIElement](
			unsafe.Pointer(element), &uixaml.IID_IUIElement)
		if err == nil {
			elements = append(elements, owned)
		}
		return true
	})
	defer func() {
		for _, element := range elements {
			element.Release()
		}
	}()

	for _, element := range elements {
		peer, err := peerOf(element)
		if err != nil || peer == nil {
			continue
		}
		entry := control{}
		name, err := peer.GetName()
		if err == nil {
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
			peer.Release()
			continue
		}
		result.Live++

		// Drive the first pattern offered, comparing the page's status text either
		// side. Text is the observable every page in this gallery already produces,
		// which is what makes one rule work across all of them.
		before := allStatus(root)
		for _, candidate := range drivable {
			if candidate.name != entry.Patterns[0] {
				continue
			}
			entry.Driven = candidate.name
			entry.Failure = candidate.drive(peer)
			break
		}
		if entry.Failure == "" && allStatus(root) != before {
			entry.Responded = true
			result.Responsive++
		}
		result.Controls = append(result.Controls, entry)
		peer.Release()
	}
}
