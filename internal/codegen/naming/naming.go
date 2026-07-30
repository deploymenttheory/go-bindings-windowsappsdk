// Package naming holds the metadata → Go naming rules shared by every emitter.
package naming

import "strings"

// LocalRoot is the namespace prefix this module projects. It is stripped from
// package paths and import aliases, because the generated tree already lives
// under bindings/winui/ and repeating it in every path would say it twice.
const LocalRoot = "Microsoft."

// ExternalRoot is the namespace prefix go-bindings-winrt projects. That module
// strips it from its own package paths, so reproducing its import paths means
// stripping it the same way here.
const ExternalRoot = "Windows."

// externalAliasPrefix distinguishes an imported Windows.* package from a local
// Microsoft.* one.
//
// Without it the two collide. Strip the roots and this module's
// Microsoft.UI.Xaml.Interop and go-bindings-winrt's Windows.UI.Xaml.Interop are
// both "uixamlinterop" — as are UI.Xaml.Data, UI.Xaml.Markup, UI.Text and UI
// itself. WinUI 3 is a fork of the UWP XAML framework, so the parallel naming
// is not a coincidence and will not go away.
//
// A prefix rather than a suffix so the alias sorts and reads as "the WinRT one".
// Three letters, and no local namespace can produce it: that would need a
// Microsoft.Wrt* namespace to exist.
const externalAliasPrefix = "wrt"

// goReservedWords are identifiers a parameter may not use: Go keywords plus
// predeclared identifiers and names the generated code itself binds (imports
// and locals in generated bodies).
var goReservedWords = map[string]bool{
	// keywords
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	// predeclared
	"any": true, "bool": true, "byte": true, "error": true, "int": true,
	"int8": true, "int16": true, "int32": true, "int64": true, "rune": true,
	"string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true, "float32": true, "float64": true,
	"true": true, "false": true, "nil": true, "len": true, "cap": true,
	"new": true, "make": true, "copy": true, "append": true, "panic": true,
	"recover": true, "print": true, "println": true, "close": true, "delete": true,
	"complex": true, "complex64": true, "complex128": true, "imag": true, "real": true,
	"min": true, "max": true, "clear": true,
	// names bound by generated code
	"unsafe": true, "syscall": true, "win32": true, "syswinrt": true,
	"winrt": true, "winui": true, "err": true, "r1": true, "result": true,
	"instance": true, "factory": true, "factoryUnknown": true, // factory-constructor locals
	"self": true, // method receiver
}

// goKeywords are the Go keywords proper. A package clause may not use one
// ("package import" does not parse), so a namespace segment that lowercases to a
// keyword takes a trailing underscore. Predeclared identifiers (string, error)
// are fine as package names and stay untouched.
//
// go-bindings-winrt applies the same rule — Windows.Media.Import becomes package
// import_ in media/import_ — and its import paths have to be reproduced exactly,
// so this must not diverge from it.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// Export makes a metadata identifier usable as an exported Go package-level
// name: leading underscores are trimmed and the first letter is capitalized.
// Case-collapsed collisions are caught by the generator's per-package name
// claims.
func Export(name string) string {
	name = strings.TrimLeft(name, "_")
	if name == "" {
		return "X"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// ParamName escapes a metadata parameter name for use as a Go parameter.
func ParamName(name string) string {
	if name == "" {
		return "param"
	}
	if goReservedWords[name] {
		return name + "_"
	}
	return name
}

// packageSegment lowercases one namespace segment and escapes Go keywords so
// the segment is always usable as a package name (and its directory matches).
func packageSegment(segment string) string {
	segment = strings.ToLower(segment)
	if goKeywords[segment] {
		return segment + "_"
	}
	return segment
}

// packagePathBelow strips a root prefix and renders the rest as a directory
// path.
func packagePathBelow(root, namespace string) string {
	segments := strings.Split(strings.TrimPrefix(namespace, root), ".")
	for i, segment := range segments {
		segments[i] = packageSegment(segment)
	}
	return strings.Join(segments, "/")
}

// PackagePath converts a local namespace ("Microsoft.UI.Xaml.Controls") to the
// generated package's directory below bindings/winui ("ui/xaml/controls").
func PackagePath(namespace string) string {
	return packagePathBelow(LocalRoot, namespace)
}

// ExternalPackagePath converts a Windows.* namespace to its directory below
// go-bindings-winrt's bindings/winrt ("Windows.Foundation.Collections" →
// "foundation/collections"). It must match that module's own PackagePath, since
// the result is used to build an import path into it.
func ExternalPackagePath(namespace string) string {
	return packagePathBelow(ExternalRoot, namespace)
}

// PackageName returns the Go package name for a namespace: the lowercased final
// segment ("Microsoft.UI.Dispatching" → "dispatching"), keyword-escaped.
//
// Callers pass the namespace that *represents a package*, which since clustering is
// not every namespace: the fourteen mutually recursive XAML namespaces are all
// represented by "Microsoft.UI.Xaml", so the package is "xaml" and there is no
// "controls". See internal/codegen/pipeline/clusters.go.
func PackageName(namespace string) string {
	segments := strings.Split(namespace, ".")
	return packageSegment(segments[len(segments)-1])
}

// ImportAlias returns the alias generated files use for a cross-namespace import
// within this module. Namespaces share leaf names freely, so the alias joins
// every root-stripped segment ("Microsoft.UI.Xaml.Controls" →
// "uixamlcontrols"), keeping aliases unique per full namespace.
func ImportAlias(namespace string) string {
	return aliasBelow(LocalRoot, namespace)
}

// ExternalImportAlias returns the alias for a Windows.* import, prefixed so it
// cannot collide with a local one. See externalAliasPrefix for why that matters.
func ExternalImportAlias(namespace string) string {
	return externalAliasPrefix + aliasBelow(ExternalRoot, namespace)
}

func aliasBelow(root, namespace string) string {
	segments := strings.Split(strings.TrimPrefix(namespace, root), ".")
	return strings.ToLower(strings.Join(segments, ""))
}

// InterfaceAsName is the runtime-class query-method name for an interface: "As"
// plus the interface name with its I prefix stripped (IButtonBase →
// AsButtonBase).
func InterfaceAsName(interfaceName string) string {
	return "As" + Export(trimInterfacePrefix(interfaceName))
}

// StaticsAccessorName is the package-level accessor name for a class's statics
// interface: the interface name with its I prefix stripped (IButtonStatics →
// ButtonStatics).
func StaticsAccessorName(interfaceName string) string {
	return Export(trimInterfacePrefix(interfaceName))
}

// trimInterfacePrefix strips the WinRT interface I prefix when the name follows
// the ICapitalized convention.
func trimInterfacePrefix(interfaceName string) string {
	if len(interfaceName) >= 2 && interfaceName[0] == 'I' && interfaceName[1] >= 'A' && interfaceName[1] <= 'Z' {
		return interfaceName[1:]
	}
	return interfaceName
}
