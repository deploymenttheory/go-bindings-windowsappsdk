package main

// The validate stage: structural integrity checks over the committed IR.
//
// This is what CI runs to prove the committed metadata is coherent before
// anything is generated from it. Errors fail the process; warnings report, and
// exist for the cases where the metadata is legitimately shaped that way.

import (
	"flag"
	"fmt"
	"sort"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/ingest"
)

func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	metadataDir := flags.String("metadata", defaultMetadataDir(), "directory of .wasdkmeta.json files")
	checkExternal := flags.Bool("external", false,
		"also require every Windows.* reference to resolve against the pinned go-bindings-winrt")
	winrtMetadata := flags.String("winrt-metadata", "", "override the Windows.* metadata directory")
	if err := flags.Parse(args); err != nil {
		return err
	}

	namespaces, err := wasdkmeta.ReadAll(*metadataDir)
	if err != nil {
		return err
	}
	if len(namespaces) == 0 {
		return fmt.Errorf("no %s files in %s (run 'generate ingest')", wasdkmeta.Extension, *metadataDir)
	}

	var externalSet *external.Set
	if *checkExternal {
		if externalSet, err = external.Load(*winrtMetadata); err != nil {
			return err
		}
	}

	report := &validation{local: map[string]bool{}}
	for _, meta := range namespaces {
		report.local[meta.Namespace] = true
	}
	for _, meta := range namespaces {
		report.checkNamespace(meta, externalSet)
	}

	sort.Strings(report.warnings)
	sort.Strings(report.errors)
	for _, warning := range report.warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	for _, message := range report.errors {
		fmt.Printf("error: %s\n", message)
	}
	fmt.Printf("validate: %d namespaces, %d errors, %d warnings\n",
		len(namespaces), len(report.errors), len(report.warnings))
	if externalSet != nil {
		fmt.Printf("external: %d references resolved against %s %s\n",
			report.externalResolved, external.ModulePath, externalSet.Version)
	}
	if len(report.errors) > 0 {
		return fmt.Errorf("%d validation errors", len(report.errors))
	}
	return nil
}

// validation accumulates findings across namespaces.
type validation struct {
	local            map[string]bool
	errors           []string
	warnings         []string
	externalResolved int
}

func (v *validation) errorf(format string, args ...any) {
	v.errors = append(v.errors, fmt.Sprintf(format, args...))
}

func (v *validation) warnf(format string, args ...any) {
	v.warnings = append(v.warnings, fmt.Sprintf(format, args...))
}

func (v *validation) checkNamespace(meta *wasdkmeta.NamespaceMeta, externalSet *external.Set) {
	namespace := meta.Namespace

	wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
		if ref.Kind != "ApiRef" && ref.Kind != "GenericInst" {
			return
		}
		if ref.Namespace == "" {
			v.errorf("[%s] %s reference %q has no namespace", namespace, ref.Kind, ref.Name)
			return
		}
		switch {
		case ref.External:
			v.checkExternalRef(namespace, ref, externalSet)
		case v.local[ref.Namespace]:
			v.checkLocalRef(namespace, ref)
		default:
			// Neither local nor external. Permanent absences are known and
			// tolerated; anything else means the pinned winmd set is
			// incomplete, which no amount of generator work can paper over.
			if _, foreign := ingest.KnownForeignNamespaces[ref.Namespace]; !foreign {
				v.errorf("[%s] reference to unknown namespace %s (%s) — it is neither ingested here "+
					"nor in %s; the winmd pin is incomplete",
					namespace, ref.Namespace, ref.Name, external.ModulePath)
			}
		}
	})

	for name, enum := range meta.Enums {
		if enum.BaseType != "int32" && enum.BaseType != "uint32" {
			v.errorf("[%s] enum %s has invalid base type %q (WinRT enums are Int32 or UInt32)", namespace, name, enum.BaseType)
		}
		if len(enum.Members) == 0 {
			v.warnf("[%s] enum %s has no members", namespace, name)
		}
	}
	for name, definition := range meta.Interfaces {
		// An interface with no IID cannot be queried for, so a generated
		// QueryInterface against it would pass a zero GUID.
		if definition.GUID == "" {
			v.errorf("[%s] interface %s has no [Guid]; it cannot be queried for", namespace, name)
		}
	}
	for name, delegate := range meta.Delegates {
		if delegate.GUID == "" {
			v.errorf("[%s] delegate %s has no [Guid]", namespace, name)
		}
		if delegate.Invoke.Name == "" {
			v.errorf("[%s] delegate %s has no Invoke method", namespace, name)
		}
	}
	for name, class := range meta.Classes {
		if class.DefaultInterface == nil && len(class.Interfaces) > 0 {
			v.warnf("[%s] class %s has instance interfaces but no [Default]", namespace, name)
		}
	}
}

// checkLocalRef requires the target to exist in the ingested tree with the kind
// the reference claims. A dangling reference here is a projection bug: the
// winmds define the type, so failing to record it means ingest dropped it.
func (v *validation) checkLocalRef(from string, ref *wasdkmeta.TypeRef) {
	if ref.TargetKind == "" {
		v.errorf("[%s] reference %s.%s has no target kind (unresolved at ingest)", from, ref.Namespace, ref.Name)
	}
}

// checkExternalRef requires the target to exist in the pinned
// go-bindings-winrt, with the same kind. This is the check that stops a
// generated file naming a Go type that module never emitted — which would
// otherwise surface as a compile error across dozens of packages with no
// indication of which reference caused it.
func (v *validation) checkExternalRef(from string, ref *wasdkmeta.TypeRef, externalSet *external.Set) {
	if externalSet == nil {
		return // --external not requested
	}
	if !externalSet.Defines(ref.Namespace) {
		v.errorf("[%s] %s.%s is marked external but %s carries no such namespace",
			from, ref.Namespace, ref.Name, external.ModulePath)
		return
	}
	kind := externalSet.Kind(ref.Namespace + "." + ref.Name)
	switch {
	case kind == "":
		// WinRT contracts only ever add, so a newer contract always resolves an
		// older assembly's references. A miss therefore means the Windows App
		// SDK metadata is newer than the pinned module, and the fix is to bump
		// it rather than to work around the reference.
		v.errorf("[%s] %s.%s is not in %s %s — the Windows App SDK metadata is newer than the pin; bump it",
			from, ref.Namespace, ref.Name, external.ModulePath, externalSet.Version)
	case ref.TargetKind != "" && kind != ref.TargetKind:
		v.errorf("[%s] %s.%s is a %s here but a %s in %s",
			from, ref.Namespace, ref.Name, ref.TargetKind, kind, external.ModulePath)
	default:
		v.externalResolved++
	}
}
