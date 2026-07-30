package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	// Deliberately unsorted, with a duplicate.
	diagnostics := []string{
		"generic-member-skipped: Microsoft.UI.Xaml.IFoo.Bar",
		"import-cycle-skipped: Microsoft.UI.Xaml.IBaz.Qux",
		"generic-member-skipped: Microsoft.UI.Xaml.IFoo.Bar",
	}
	if err := WriteBaseline(path, diagnostics); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	newEntries, err := CheckBaseline(path, diagnostics)
	if err != nil {
		t.Fatalf("CheckBaseline: %v", err)
	}
	if len(newEntries) != 0 {
		t.Errorf("the baseline it just wrote reports %d new entries: %v", len(newEntries), newEntries)
	}
}

// TestBaselineIsSortedAndDeduplicated keeps the committed file reviewable: it is
// read as a diff, so the same set of diagnostics has to produce the same bytes
// however the generator happened to encounter them.
func TestBaselineIsSortedAndDeduplicated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaseline(path, []string{"zebra: z", "alpha: a", "alpha: a", "middle: m"}); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	if strings.Count(source, "alpha: a") != 1 {
		t.Errorf("the duplicate was not collapsed:\n%s", source)
	}
	alphaAt := strings.Index(source, "alpha")
	middleAt := strings.Index(source, "middle")
	zebraAt := strings.Index(source, "zebra")
	if !(alphaAt < middleAt && middleAt < zebraAt) {
		t.Errorf("entries are not sorted:\n%s", source)
	}
	if !strings.HasSuffix(source, "\n") {
		t.Error("the file does not end with a newline")
	}
}

// TestRatchetCatchesNewDegradations is the whole point: a projection this size
// always has members it cannot represent, so the useful question is not "are
// there any" but "are there more than last time".
func TestRatchetCatchesNewDegradations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaseline(path, []string{"known: one", "known: two"}); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	newEntries, err := CheckBaseline(path, []string{"known: one", "known: two", "regression: three"})
	if err != nil {
		t.Fatalf("CheckBaseline: %v", err)
	}
	if len(newEntries) != 1 || newEntries[0] != "regression: three" {
		t.Errorf("new entries = %v, want just the regression", newEntries)
	}
}

// TestFewerDegradationsIsNotAFailure is the other direction. Fixing a degradation
// must not fail CI; the baseline shrinks on the next rewrite, and until then a
// smaller set is simply within it.
func TestFewerDegradationsIsNotAFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaseline(path, []string{"known: one", "known: two"}); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	newEntries, err := CheckBaseline(path, []string{"known: one"})
	if err != nil {
		t.Fatalf("CheckBaseline: %v", err)
	}
	if len(newEntries) != 0 {
		t.Errorf("fixing a degradation reported %v as new", newEntries)
	}
}

func TestCheckBaselineMissingFile(t *testing.T) {
	if _, err := CheckBaseline(filepath.Join(t.TempDir(), "absent.json"), nil); err == nil {
		t.Fatal("CheckBaseline on a missing file returned no error")
	}
}

func TestCheckBaselineMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CheckBaseline(path, nil)
	if err == nil {
		t.Fatal("CheckBaseline accepted a malformed file")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the file", err)
	}
}
