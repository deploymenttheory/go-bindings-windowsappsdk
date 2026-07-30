package naming

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

func TestExport(t *testing.T) {
	for input, want := range map[string]string{
		"IWindow":    "IWindow",
		"button":     "Button",
		"_reserved":  "Reserved",
		"__":         "X",
		"":           "X",
		"get_Flyout": "Get_Flyout",
	} {
		if got := Export(input); got != want {
			t.Errorf("Export(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParamName(t *testing.T) {
	for input, want := range map[string]string{
		"value":     "value",
		"type":      "type_",   // Go keyword
		"string":    "string_", // predeclared
		"self":      "self_",   // the generated method receiver
		"winui":     "winui_",  // a package the generated code imports
		"":          "param",
		"innerType": "innerType",
	} {
		if got := ParamName(input); got != want {
			t.Errorf("ParamName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPackagePathAndName(t *testing.T) {
	for namespace, want := range map[string]string{
		"Microsoft.UI.Xaml":                     "ui/xaml",
		"Microsoft.UI.Xaml.Controls":            "ui/xaml/controls",
		"Microsoft.UI.Xaml.Controls.Primitives": "ui/xaml/controls/primitives",
		"Microsoft.UI.Dispatching":              "ui/dispatching",
		"Microsoft.Windows.AppLifecycle":        "windows/applifecycle",
		"Microsoft.Windows.Storage.Pickers":     "windows/storage/pickers",
	} {
		if got := PackagePath(namespace); got != want {
			t.Errorf("PackagePath(%q) = %q, want %q", namespace, got, want)
		}
	}
	for namespace, want := range map[string]string{
		"Microsoft.UI.Xaml":          "xaml",
		"Microsoft.UI.Xaml.Controls": "controls",
		"Microsoft.UI":               "ui",
	} {
		if got := PackageName(namespace); got != want {
			t.Errorf("PackageName(%q) = %q, want %q", namespace, got, want)
		}
	}
}

// TestKeywordSegmentsAreEscaped covers the case that does not parse: a directory
// whose package clause would read "package interface".
func TestKeywordSegmentsAreEscaped(t *testing.T) {
	if got := PackagePath("Microsoft.Fake.Import"); got != "fake/import_" {
		t.Errorf("PackagePath = %q, want fake/import_", got)
	}
	if got := PackageName("Microsoft.Fake.Import"); got != "import_" {
		t.Errorf("PackageName = %q, want import_", got)
	}
	// Predeclared identifiers are legal package names and must be left alone.
	if got := PackageName("Microsoft.Fake.String"); got != "string" {
		t.Errorf("PackageName = %q, want string (predeclared names are fine)", got)
	}
}

// TestExternalAliasAvoidsTheCollision is the reason ExternalImportAlias exists.
//
// WinUI 3 is a fork of the UWP XAML framework, so the two namespace trees run in
// parallel. Strip the roots and this module's Microsoft.UI.Xaml.Interop and
// go-bindings-winrt's Windows.UI.Xaml.Interop are the same identifier — and so
// are UI.Xaml.Data, UI.Xaml.Markup, UI.Text and UI itself. Left alone, a package
// importing both would not compile, across roughly thirty packages on the first
// full run.
func TestExternalAliasAvoidsTheCollision(t *testing.T) {
	collidingLeaves := []string{
		"UI",
		"UI.Text",
		"UI.Xaml",
		"UI.Xaml.Data",
		"UI.Xaml.Interop",
		"UI.Xaml.Markup",
		"UI.Xaml.Controls",
		"UI.Xaml.Media",
		"UI.Xaml.Input",
	}
	for _, leaf := range collidingLeaves {
		local := ImportAlias(LocalRoot + leaf)
		foreign := ExternalImportAlias(ExternalRoot + leaf)
		if local == foreign {
			t.Errorf("%s: local and external aliases are both %q", leaf, local)
		}
		if foreign != "wrt"+local {
			t.Errorf("%s: external alias = %q, want wrt%s", leaf, foreign, local)
		}
	}
}

// TestAliasesAreUniquePerNamespace guards the other half: aliases join every
// segment precisely because leaf names repeat, so two different namespaces must
// never land on one alias.
func TestAliasesAreUniquePerNamespace(t *testing.T) {
	namespaces := []string{
		"Microsoft.UI.Xaml",
		"Microsoft.UI.Xaml.Controls",
		"Microsoft.UI.Xaml.Controls.Primitives",
		"Microsoft.UI.Xaml.Media",
		"Microsoft.UI.Xaml.Media.Animation",
		"Microsoft.UI.Text",
		"Microsoft.Windows.Storage",
		"Microsoft.Windows.Storage.Pickers",
	}
	seen := map[string]string{}
	for _, namespace := range namespaces {
		alias := ImportAlias(namespace)
		if previous, clash := seen[alias]; clash {
			t.Errorf("alias %q is shared by %s and %s", alias, previous, namespace)
		}
		seen[alias] = namespace
	}
}

// TestExternalPackagePathMatchesTheRealModule is the check that cannot be made
// by inspection: the computed path is used to build an import into
// go-bindings-winrt, so it has to name a directory that is actually there.
//
// Every namespace in the pinned module is tested rather than a sample, because
// the failure mode is one namespace out of 282 whose path this derives
// differently — a keyword segment, say — and that is invisible until the emit
// stage names it.
func TestExternalPackagePathMatchesTheRealModule(t *testing.T) {
	metadataDir, version, err := external.Locate()
	if err != nil {
		t.Fatalf("locating the pinned module: %v", err)
	}
	set, err := external.Load(metadataDir)
	if err != nil {
		t.Fatalf("loading the Windows.* metadata: %v", err)
	}
	// metadata/winrt/.. → the module root, then its generated bindings tree.
	bindingsRoot := filepath.Join(filepath.Dir(filepath.Dir(metadataDir)), "bindings", "winrt")
	if _, err := os.Stat(bindingsRoot); err != nil {
		t.Skipf("%s has no generated bindings tree to check against: %v", version, err)
	}

	namespaces := set.Namespaces()
	if len(namespaces) == 0 {
		t.Fatal("the pinned module defines no namespaces")
	}
	for _, namespace := range namespaces {
		path := ExternalPackagePath(namespace)
		dir := filepath.Join(bindingsRoot, filepath.FromSlash(path))
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s → %q, but %s does not exist in %s",
				namespace, path, dir, version)
		}
	}
	t.Logf("%d namespace paths verified against %s", len(namespaces), version)
}

func TestInterfaceAccessorNames(t *testing.T) {
	for input, want := range map[string]string{
		"IButtonBase":     "AsButtonBase",
		"IContentControl": "AsContentControl",
		"IWindow":         "AsWindow",
		// Not the ICapitalized convention, so the I is part of the name.
		"Inspectable": "AsInspectable",
		"IID":         "AsID",
	} {
		if got := InterfaceAsName(input); got != want {
			t.Errorf("InterfaceAsName(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"IButtonStatics":      "ButtonStatics",
		"IApplicationStatics": "ApplicationStatics",
	} {
		if got := StaticsAccessorName(input); got != want {
			t.Errorf("StaticsAccessorName(%q) = %q, want %q", input, got, want)
		}
	}
}
