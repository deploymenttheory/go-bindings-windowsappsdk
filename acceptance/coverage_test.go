//go:build windows && amd64

package acceptance

// How much of the corpus has an assertion behind it?
//
// TestGalleryPagesLayOut proves every page builds. TestPagesBehaveWhenDriven proves that a
// handful of them DO something. Nothing connected the two, so "this page is asserted" and
// "nobody has got to this page yet" looked identical — and a corpus of 202 pages cannot be
// held to a standard that is invisible.
//
// A page satisfies this suite by one of two routes:
//
//   - a scenario in zz_behaviour_test.go names it, so its behaviour is driven and checked
//     on every run; or
//   - Page.Inert says why it has none.
//
// Inert is not a waiver. The bar for this corpus is FIDELITY TO ITS SOURCE — a page wires
// the handlers its upstream TestUI page wires, no more and no fewer — so a styling fixture
// or an accessibility-scanner target legitimately has nothing to assert, and saying so is
// the honest answer rather than inventing an interaction upstream never had.
//
// The ratchet is the point. The number of pages with NEITHER may only fall. That converts
// "we should cover more of this" from an intention into something a build fails over.
//
// # DO NOT SOURCE Inert REASONS FROM THE CENSUS
//
// The census reports zero live controls for every NavigationView, Repeater and ItemsView
// page in the corpus, and NONE of them is inert: a NavigationView's menu items and a
// repeater's rows are realized into generated containers, and the census walks the LOGICAL
// tree, which deliberately stops at a control rather than descending into what it
// generates. Those pages are unmeasured, not uninteractive.
//
// Writing "zero live controls, therefore Inert" over that batch would have excused 37
// pages in one commit and enshrined a limitation of the harness as a claim about the port.
// An Inert reason has to say why the SOURCE page has no interaction — which is a question
// about the upstream XAML, not about what an automation peer happened to reach.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
)

// coverageBaselinePath holds the number of pages still lacking both an assertion and a
// reason. Committed, so a rise is a reviewed diff rather than a silent regression.
var coverageBaselinePath = filepath.Join("..", "metadata", "behaviour-coverage.json")

type coverageBaseline struct {
	// Uncovered is the count of buildable pages with neither a scenario nor an Inert
	// reason. It may only decrease.
	Uncovered int `json:"uncovered"`
	// Note explains the number to whoever sees it move.
	Note string `json:"note"`
}

// minimumReasonLength is the same bar gallery_test.go applies to Unmappable: a reason
// short enough to be a shrug is not a reason.
const minimumReasonLength = 10

func TestEveryPageIsAssertedOrExcused(t *testing.T) {
	asserted := map[string]bool{}
	for _, entry := range scenarios {
		asserted[entry.page] = true
	}

	var uncovered []string
	for _, page := range pages.Buildable() {
		key := page.Key()
		switch {
		case asserted[key]:
			// Driven and checked by the behaviour suite.
		case strings.TrimSpace(page.Inert) == "":
			uncovered = append(uncovered, key)
		case len(strings.TrimSpace(page.Inert)) < minimumReasonLength:
			t.Errorf("%s: Inert reason is too short to be one: %q", key, page.Inert)
		}
	}
	sort.Strings(uncovered)

	baseline := readCoverageBaseline(t)
	switch {
	case len(uncovered) > baseline.Uncovered:
		t.Errorf("pages with neither an assertion nor a reason rose from %d to %d.\n"+
			"Add a scenario in zz_behaviour_test.go, or set Page.Inert saying why the page\n"+
			"has no behaviour to assert — and remember the bar is fidelity to the source:\n"+
			"if the upstream page wires no handler, Inert is the correct answer.\n"+
			"First few: %s",
			baseline.Uncovered, len(uncovered), strings.Join(first(uncovered, 5), ", "))
	case len(uncovered) < baseline.Uncovered:
		t.Errorf("pages with neither an assertion nor a reason fell from %d to %d — "+
			"good, now lower the baseline in %s to hold the gain",
			baseline.Uncovered, len(uncovered), coverageBaselinePath)
	default:
		t.Logf("%d of %d pages asserted or excused; %d still to do",
			len(pages.Buildable())-len(uncovered), len(pages.Buildable()), len(uncovered))
	}
}

func readCoverageBaseline(t *testing.T) coverageBaseline {
	t.Helper()
	raw, err := os.ReadFile(coverageBaselinePath)
	if err != nil {
		t.Fatalf("reading %s: %v", coverageBaselinePath, err)
	}
	var baseline coverageBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("decoding %s: %v", coverageBaselinePath, err)
	}
	return baseline
}

func first(values []string, n int) []string {
	if len(values) < n {
		return values
	}
	return values[:n]
}
