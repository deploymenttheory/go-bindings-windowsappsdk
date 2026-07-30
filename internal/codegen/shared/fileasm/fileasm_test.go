package fileasm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssembleScaffold(t *testing.T) {
	got, err := Assemble(File{
		PackageName: "controls",
		BuildTag:    GeneratedBuildTag,
		Body:        "// Button is a control.\ntype Button struct{}\n",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	source := string(got)
	// The header must be first: pruning keys off it, so a file whose header has
	// moved would be treated as hand-written and never cleaned up.
	if !strings.HasPrefix(source, Header) {
		t.Errorf("the file does not start with the DO-NOT-EDIT header:\n%s", source)
	}
	for _, want := range []string{
		"//go:build " + GeneratedBuildTag,
		"package controls",
		"type Button struct{}",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("missing %q in:\n%s", want, source)
		}
	}
	// The build tag must be separated from the package clause by a blank line, or
	// the toolchain reads it as a doc comment and ignores the constraint.
	if !strings.Contains(source, "&& amd64\n\npackage controls") {
		t.Errorf("no blank line between the build tag and the package clause:\n%s", source)
	}
}

// TestImportsAreGroupedAndSorted keeps the output stable. Imports arrive from a
// map, so unsorted iteration would make regeneration produce a different file
// every run.
func TestImportsAreGroupedAndSorted(t *testing.T) {
	got, err := Assemble(File{
		PackageName: "controls",
		BuildTag:    GeneratedBuildTag,
		Imports: map[string]string{
			"unsafe":         "unsafe",
			"syscall":        "syscall",
			"winrt":          "github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt",
			"win32":          "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32",
			"wrtfoundation":  "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation",
			"uixamlcontrols": "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls",
		},
		Body: "var _ = unsafe.Pointer(nil)\n",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	source := string(got)
	stdlibAt := strings.Index(source, `"syscall"`)
	moduleAt := strings.Index(source, "go-bindings-win32")
	if stdlibAt < 0 || moduleAt < 0 {
		t.Fatalf("imports missing from:\n%s", source)
	}
	if stdlibAt > moduleAt {
		t.Errorf("the standard library group comes after the module group:\n%s", source)
	}
	// An alias equal to the path's last element is redundant and gofmt-noise.
	if strings.Contains(source, `winrt "github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"`) {
		t.Error("a redundant alias was written out")
	}
	// One that differs must be kept, or the body would reference an unimported name.
	if !strings.Contains(source, `wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"`) {
		t.Errorf("the prefixed external alias was dropped:\n%s", source)
	}
}

// TestAssembleIsDeterministic is the property CI enforces on the whole tree, so
// it is worth pinning at the level where map iteration actually happens.
func TestAssembleIsDeterministic(t *testing.T) {
	file := File{
		PackageName: "xaml",
		BuildTag:    GeneratedBuildTag,
		Imports: map[string]string{
			"a": "example.com/a", "b": "example.com/b", "c": "example.com/c",
			"d": "example.com/d", "e": "example.com/e", "syscall": "syscall",
		},
		Body: "var _ = 0\n",
	}
	first, err := Assemble(file)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for range 8 {
		again, err := Assemble(file)
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		if string(again) != string(first) {
			t.Fatal("Assemble is not deterministic across runs")
		}
	}
}

// TestUnparseableBodyIsReported catches an emitter bug at the point of emission,
// naming the package, rather than leaving a broken file for `go build` to find.
func TestUnparseableBodyIsReported(t *testing.T) {
	_, err := Assemble(File{
		PackageName: "broken",
		BuildTag:    GeneratedBuildTag,
		Body:        "type Button struct{\n",
	})
	if err == nil {
		t.Fatal("Assemble accepted a body that does not parse")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error = %q, want it to name the package", err)
	}
}

func TestWriteGoFileCreatesParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui", "xaml", "controls", "controls_classes.go")
	err := WriteGoFile(path, File{
		PackageName: "controls",
		BuildTag:    GeneratedBuildTag,
		Body:        "type Button struct{}\n",
	})
	if err != nil {
		t.Fatalf("WriteGoFile: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !strings.HasPrefix(string(content), Header) {
		t.Error("the written file does not carry the header")
	}
}

// TestBuildTagExcludesTheArchitecturesItMustExclude states the constraint as an
// assertion. arm64 is excluded because Go's asm_windows_arm64.s never loads the V
// registers, so every double and every all-float aggregate crossing
// syscall.SyscallN would be silently corrupted — and XAML's surface is full of
// both.
func TestBuildTagExcludesTheArchitecturesItMustExclude(t *testing.T) {
	if !strings.Contains(GeneratedBuildTag, "amd64") {
		t.Errorf("build tag %q does not name amd64", GeneratedBuildTag)
	}
	for _, excluded := range []string{"arm64", "386"} {
		if strings.Contains(GeneratedBuildTag, excluded) {
			t.Errorf("build tag %q admits %s", GeneratedBuildTag, excluded)
		}
	}
	if !strings.Contains(GeneratedBuildTag, "windows") {
		t.Errorf("build tag %q does not constrain the OS", GeneratedBuildTag)
	}
}

// TestHeaderNamesThisGenerator matters because the emitter prunes stale output by
// matching this exact string. A header naming a sibling repository's generator
// would make it delete, or refuse to delete, the wrong files.
func TestHeaderNamesThisGenerator(t *testing.T) {
	if !strings.Contains(Header, "go-bindings-windowsappsdk") {
		t.Errorf("Header = %q, want it to name this module's generator", Header)
	}
	if !strings.Contains(Header, "DO NOT EDIT") {
		t.Errorf("Header = %q, want the conventional DO NOT EDIT marker", Header)
	}
}
