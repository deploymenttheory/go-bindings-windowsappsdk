// Command inspect reads a winmd and prints what is in it.
//
// It answers the questions that come up constantly while writing a projection:
// which namespaces does this file define, what is this interface's IID, and
// which vtable slot is this method at. Those are needed by hand before any
// generator exists, and remain the quickest way to check what the generator
// produced afterwards.
//
// Usage:
//
//	go run ./cmd/inspect --winmd metadata/winmd/Microsoft.UI.Xaml.winmd
//	go run ./cmd/inspect --winmd <file> --namespaces
//	go run ./cmd/inspect --winmd <file> --type Microsoft.UI.Xaml.IWindow
//	go run ./cmd/inspect --dir metadata/winmd --namespaces
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	winmd "github.com/deploymenttheory/go-winmd"
)

func main() {
	file := flag.String("winmd", "", "winmd file to read")
	dir := flag.String("dir", "", "read every winmd in this directory")
	namespaces := flag.Bool("namespaces", false, "list namespaces and how many types each holds")
	typeName := flag.String("type", "", "print one type: its IID and its methods with vtable slots")
	search := flag.String("search", "", "list types whose full name contains this")
	refs := flag.Bool("refs", false, "list namespaces referenced but not defined here")
	flag.Parse()

	if *file == "" && *dir == "" {
		flag.Usage()
		os.Exit(2)
	}

	paths, err := gather(*file, *dir)
	if err != nil {
		fail(err)
	}
	switch {
	case *typeName != "":
		err = printType(paths, *typeName)
	case *search != "":
		err = printSearch(paths, *search)
	case *refs:
		err = printRefs(paths)
	case *namespaces:
		err = printNamespaces(paths)
	default:
		err = printSummary(paths)
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "inspect:", err)
	os.Exit(1)
}

func gather(file, dir string) ([]string, error) {
	if file != "" {
		return []string{file}, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.winmd"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no winmds in %s", dir)
	}
	sort.Strings(matches)
	return matches, nil
}

// printSummary reports each file's type counts, split the way a projection
// cares about: interfaces, runtime classes, enums, structs and delegates are
// all emitted differently.
func printSummary(paths []string) error {
	fmt.Printf("%-56s %8s %6s %6s %6s %6s %6s\n",
		"WINMD", "TYPES", "IFACE", "CLASS", "ENUM", "STRUCT", "DELEG")
	var totals [5]int
	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		var counts [5]int
		for i := range file.Tables.TypeDefs {
			switch classify(file, &file.Tables.TypeDefs[i]) {
			case kindInterface:
				counts[0]++
			case kindClass:
				counts[1]++
			case kindEnum:
				counts[2]++
			case kindStruct:
				counts[3]++
			case kindDelegate:
				counts[4]++
			}
		}
		for i := range counts {
			totals[i] += counts[i]
		}
		fmt.Printf("%-56s %8d %6d %6d %6d %6d %6d\n", filepath.Base(path),
			len(file.Tables.TypeDefs), counts[0], counts[1], counts[2], counts[3], counts[4])
	}
	if len(paths) > 1 {
		fmt.Printf("%-56s %8s %6d %6d %6d %6d %6d\n", "TOTAL", "",
			totals[0], totals[1], totals[2], totals[3], totals[4])
	}
	return nil
}

// printNamespaces lists every namespace defined across the files, with the
// number of public types in each. This is the shape of the eventual package
// tree.
func printNamespaces(paths []string) error {
	counts := map[string]int{}
	source := map[string]string{}
	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		for i := range file.Tables.TypeDefs {
			row := &file.Tables.TypeDefs[i]
			if row.Namespace == "" || classify(file, row) == kindOther {
				continue
			}
			counts[row.Namespace]++
			if source[row.Namespace] == "" {
				source[row.Namespace] = filepath.Base(path)
			}
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%-60s %6s  %s\n", "NAMESPACE", "TYPES", "FROM")
	for _, name := range names {
		fmt.Printf("%-60s %6d  %s\n", name, counts[name], source[name])
	}
	fmt.Printf("\n%d namespaces\n", len(names))
	return nil
}

// printRefs lists the namespaces these files reference but do not define.
//
// This is the external dependency set the projection has to resolve: anything
// under Windows.* has to map onto go-bindings-winrt's already-generated
// packages, and anything under neither Windows.* nor Microsoft.* has no Go
// equivalent at all and its users must be skipped deliberately.
func printRefs(paths []string) error {
	defined := map[string]bool{}
	referenced := map[string]int{}

	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		for i := range file.Tables.TypeDefs {
			if namespace := file.Tables.TypeDefs[i].Namespace; namespace != "" {
				defined[namespace] = true
			}
		}
	}
	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		for i := range file.Tables.TypeRefs {
			namespace := file.Tables.TypeRefs[i].Namespace
			if namespace == "" || defined[namespace] {
				continue
			}
			referenced[namespace]++
		}
	}

	names := make([]string, 0, len(referenced))
	for name := range referenced {
		names = append(names, name)
	}
	sort.Strings(names)

	var windows, system, other int
	fmt.Printf("%-56s %6s  %s\n", "REFERENCED NAMESPACE", "REFS", "RESOLVES TO")
	for _, name := range names {
		var resolution string
		switch {
		case name == "System" || strings.HasPrefix(name, "System."):
			resolution = "metadata plumbing (not projected)"
			system++
		case strings.HasPrefix(name, "Windows."):
			resolution = "go-bindings-winrt"
			windows++
		default:
			resolution = "NO GO EQUIVALENT"
			other++
		}
		fmt.Printf("%-56s %6d  %s\n", name, referenced[name], resolution)
	}
	fmt.Printf("\n%d external namespaces: %d Windows.* (go-bindings-winrt), %d System.*, %d unresolved\n",
		len(names), windows, system, other)
	return nil
}

func printSearch(paths []string, needle string) error {
	lower := strings.ToLower(needle)
	var hits int
	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		for i := range file.Tables.TypeDefs {
			row := &file.Tables.TypeDefs[i]
			full := fullName(row)
			if !strings.Contains(strings.ToLower(full), lower) {
				continue
			}
			kind := classify(file, row)
			if kind == kindOther {
				continue
			}
			fmt.Printf("%-12s %-64s %s\n", kind, full, filepath.Base(path))
			hits++
		}
	}
	fmt.Printf("\n%d types\n", hits)
	return nil
}

// printType prints one type's IID and its methods with the vtable slot each
// will dispatch through.
func printType(paths []string, wanted string) error {
	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		for i := range file.Tables.TypeDefs {
			row := &file.Tables.TypeDefs[i]
			if !strings.EqualFold(fullName(row), wanted) {
				continue
			}
			describe(file, row, uint32(i+1), filepath.Base(path))
			return nil
		}
	}
	return fmt.Errorf("type %q not found", wanted)
}

func describe(file *winmd.File, row *winmd.TypeDefRow, rowIndex uint32, from string) {
	kind := classify(file, row)
	fmt.Printf("%s\n", fullName(row))
	fmt.Printf("  kind:  %s\n", kind)
	fmt.Printf("  from:  %s\n", from)
	if guid, ok := typeGUID(file, rowIndex); ok {
		fmt.Printf("  IID:   %s\n", guid)
	}

	attributes := file.AttributesFor(winmd.CodedIndex{Table: winmd.TableTypeDef, Row: rowIndex})
	var names []string
	for _, attribute := range attributes {
		if attribute.Name == "GuidAttribute" {
			continue
		}
		names = append(names, strings.TrimSuffix(attribute.Name, "Attribute"))
	}
	if len(names) > 0 {
		sort.Strings(names)
		fmt.Printf("  attrs: %s\n", strings.Join(names, ", "))
	}

	// Slot numbering differs by kind, and getting it wrong is a call into the
	// wrong function rather than an error:
	//
	//   - An interface derives from IInspectable, which occupies slots 0-5, so
	//     its own methods start at 6 in MethodDef order.
	//   - A delegate derives from IUnknown only. Its vtable is
	//     {QueryInterface, AddRef, Release, Invoke}, so Invoke is slot 3 — and
	//     the .ctor the metadata also declares is never dispatched at all.
	//   - A runtime class has no vtable of its own. It is reached through its
	//     interfaces, so its members are listed without slots.
	first, end := row.MethodFirst, row.MethodEnd
	if first == 0 || end <= first {
		fmt.Println("  methods: none")
		return
	}
	switch kind {
	case kindInterface:
		fmt.Printf("  methods (%d), slot = 6 + MethodDef index (past IInspectable):\n", end-first)
	case kindDelegate:
		fmt.Printf("  methods (%d), Invoke at slot 3 (past IUnknown):\n", end-first)
	default:
		fmt.Printf("  methods (%d), no vtable of its own:\n", end-first)
	}
	for index := first; index < end; index++ {
		method := &file.Tables.Methods[index-1]
		switch {
		case kind == kindInterface:
			fmt.Printf("    slot %-3d %s\n", 6+(index-first), method.Name)
		case kind == kindDelegate && method.Name == "Invoke":
			fmt.Printf("    slot %-3d %s\n", 3, method.Name)
		default:
			fmt.Printf("             %s\n", method.Name)
		}
	}
}

// typeGUID reads the WinRT GuidAttribute, whose eleven fixed arguments are the
// GUID's fields: one uint32, two uint16s, then eight bytes.
func typeGUID(file *winmd.File, rowIndex uint32) (string, bool) {
	for _, attribute := range file.AttributesFor(winmd.CodedIndex{Table: winmd.TableTypeDef, Row: rowIndex}) {
		if attribute.Name != "GuidAttribute" || len(attribute.Fixed) != 11 {
			continue
		}
		data1, ok1 := toUint(attribute.Fixed[0])
		data2, ok2 := toUint(attribute.Fixed[1])
		data3, ok3 := toUint(attribute.Fixed[2])
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		rest := make([]uint64, 8)
		valid := true
		for i := range 8 {
			value, ok := toUint(attribute.Fixed[3+i])
			if !ok {
				valid = false
				break
			}
			rest[i] = value
		}
		if !valid {
			continue
		}
		return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
			data1, data2, data3, rest[0], rest[1],
			rest[2], rest[3], rest[4], rest[5], rest[6], rest[7]), true
	}
	return "", false
}

func toUint(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint8:
		return uint64(typed), true
	case uint16:
		return uint64(typed), true
	case uint32:
		return uint64(typed), true
	case uint64:
		return typed, true
	case int8:
		return uint64(uint8(typed)), true
	case int16:
		return uint64(uint16(typed)), true
	case int32:
		return uint64(uint32(typed)), true
	case int64:
		return uint64(typed), true
	}
	return 0, false
}

type typeKind string

const (
	kindInterface typeKind = "interface"
	kindClass     typeKind = "class"
	kindEnum      typeKind = "enum"
	kindStruct    typeKind = "struct"
	kindDelegate  typeKind = "delegate"
	kindOther     typeKind = "other"
)

// classify sorts a TypeDef the way a WinRT projection has to: by its flags and
// by what it extends.
func classify(file *winmd.File, row *winmd.TypeDefRow) typeKind {
	if row.Name == "<Module>" {
		return kindOther
	}
	if row.Flags&winmd.TypeAttrInterface != 0 {
		return kindInterface
	}
	switch baseName(file, row.Extends) {
	case "System.Enum":
		return kindEnum
	case "System.ValueType":
		return kindStruct
	case "System.MulticastDelegate":
		return kindDelegate
	case "System.Attribute":
		return kindOther
	}
	return kindClass
}

func baseName(file *winmd.File, extends winmd.CodedIndex) string {
	if extends.IsNull() {
		return ""
	}
	switch extends.Table {
	case winmd.TableTypeRef:
		if int(extends.Row) <= len(file.Tables.TypeRefs) {
			ref := &file.Tables.TypeRefs[extends.Row-1]
			return ref.Namespace + "." + ref.Name
		}
	case winmd.TableTypeDef:
		if int(extends.Row) <= len(file.Tables.TypeDefs) {
			def := &file.Tables.TypeDefs[extends.Row-1]
			return def.Namespace + "." + def.Name
		}
	}
	return ""
}

func fullName(row *winmd.TypeDefRow) string {
	if row.Namespace == "" {
		return row.Name
	}
	return row.Namespace + "." + row.Name
}
