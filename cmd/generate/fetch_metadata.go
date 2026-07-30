package main

// The fetch-metadata stage: download the Windows App SDK winmds.
//
// Microsoft.WindowsAppSDK is a meta-package: it carries no metadata itself and
// depends on nine components that do. Both the component set and their versions
// change with every servicing release — in 2.3.1 the meta-package is 2.3.1 while
// Foundation is 2.3.5 — so they are read from the .nuspec rather than written
// down here.
//
// Version constraints are not uniform either. 2.3.1 pins its Runtime component
// exactly ("[2.3.1]") and gives the other eight open lower bounds ("2.3.5"), so
// anything insisting on exact pins would reject the package outright.
//
//	go run ./cmd/generate fetch-metadata [--version 2.3.1] [--out metadata/winmd] [--force]

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/deploymenttheory/go-winmd/nuget"
)

const (
	sdkPackage        = "microsoft.windowsappsdk"
	sdkPackageDisplay = "Microsoft.WindowsAppSDK"
	pinnedSDKVersion  = "2.3.1"
)

// skipWinmd matches metadata that is not a public binding surface. These are
// implementation details of the SDK itself and would otherwise be projected
// into Go packages nobody can use.
func skipWinmd(base string) bool {
	lower := strings.ToLower(base)
	for _, marker := range []string{"internal", "private", "impl."} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func runFetchMetadata(args []string) error {
	flags := flag.NewFlagSet("fetch-metadata", flag.ExitOnError)
	version := flags.String("version", pinnedSDKVersion, "Windows App SDK version")
	out := flags.String("out", filepath.Join("metadata", "winmd"), "output directory")
	force := flags.Bool("force", false, "re-download even when the version already matches")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return fetchMetadata(*version, *out, *force)
}

func fetchMetadata(version, out string, force bool) error {
	provenancePath := filepath.Join(out, "PROVENANCE.json")
	if !force && alreadyAt(provenancePath, version) {
		fmt.Printf("%s %s already fetched (--force to re-download)\n", sdkPackageDisplay, version)
		return nil
	}

	client := nuget.NewClient()
	components, err := nuget.Dependencies(client, sdkPackage, version)
	if err != nil {
		return fmt.Errorf("reading the %s dependency list: %w", sdkPackage, err)
	}
	fmt.Printf("%s %s → %d components\n", sdkPackageDisplay, version, len(components))

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	var records []nuget.Provenance
	written := map[string]string{} // winmd base name → component that supplied it
	for _, component := range components {
		componentVersion, err := component.Version()
		if err != nil {
			return err
		}
		nupkg, source, err := nuget.FetchArchive(client, strings.ToLower(component.ID), componentVersion)
		if err != nil {
			return fmt.Errorf("downloading %s %s: %w", component.ID, componentVersion, err)
		}

		winmds, err := nuget.ExtractMatching(nupkg, func(name string) bool {
			return strings.HasSuffix(strings.ToLower(name), ".winmd")
		})
		if err != nil {
			return fmt.Errorf("reading %s: %w", component.ID, err)
		}

		// A component may ship the same winmd once per Windows SDK target
		// version — InteractiveExperiences has metadata/10.0.17763.0/ and
		// metadata/10.0.18362.0/ — and the contents differ, because each
		// describes the surface available at that minimum OS version. One has
		// to be chosen; see chooseTargetPlatform.
		chosen, dropped := chooseTargetPlatform(winmds)

		var kept, skipped int
		for _, entry := range sortedKeys(chosen) {
			base := path.Base(strings.ReplaceAll(entry, "\\", "/"))
			if skipWinmd(base) {
				skipped++
				continue
			}
			if owner, clash := written[base]; clash {
				return fmt.Errorf("%s is supplied by both %s and %s", base, owner, component.ID)
			}
			content := chosen[entry]
			if err := os.WriteFile(filepath.Join(out, base), content, 0o644); err != nil {
				return err
			}
			written[base] = component.ID
			kept++
			// Provenance is keyed on the winmd's own name, not the package's:
			// a type moving between components then shows up as a reviewable
			// change of source rather than silently relocating.
			records = append(records, nuget.ProvenanceFor(component.ID, componentVersion, source, entry, content))
		}
		fmt.Printf("  %-46s %-12s %2d winmds", component.ID, componentVersion, kept)
		if skipped > 0 {
			fmt.Printf("  (%d internal skipped)", skipped)
		}
		fmt.Println()
		for _, note := range dropped {
			fmt.Printf("      %s\n", note)
		}
	}

	if len(records) == 0 {
		return fmt.Errorf("no winmds found in any component of %s %s", sdkPackageDisplay, version)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].File < records[j].File })
	if err := nuget.WriteProvenance(provenancePath, records); err != nil {
		return err
	}
	fmt.Printf("\n%d winmds → %s\n", len(records), out)
	return nil
}

// alreadyAt reports whether the committed provenance already records this
// Windows App SDK version, so a re-run is a no-op.
func alreadyAt(provenancePath, version string) bool {
	records, err := nuget.ReadProvenance(provenancePath)
	if err != nil || len(records) == 0 {
		return false
	}
	// Components carry their own versions, so the meta-package version is
	// recorded by the Runtime component, which is pinned to it exactly.
	for _, record := range records {
		if strings.EqualFold(record.Package, "Microsoft.WindowsAppSDK.Runtime") {
			return record.Version == version
		}
	}
	return false
}

// targetPlatformDir matches a Windows SDK version used as a directory name,
// e.g. "10.0.18362.0".
var targetPlatformDir = regexp.MustCompile(`^\d+(\.\d+)+$`)

// chooseTargetPlatform reduces winmds that appear once per Windows SDK target
// version to a single copy, keeping the HIGHEST version.
//
// The variants are not duplicates: 10.0.17763.0 (Windows 10 1809, the Windows
// App SDK's minimum supported OS) and 10.0.18362.0 (1903) describe different
// API surfaces, and their contents differ. Taking the highest generates the
// fullest surface, which is the right default for a binding library: a caller
// that needs to run on an older floor can decide that for itself, whereas API
// missing from the bindings cannot be recovered at all.
//
// Entries not under a version directory are passed through untouched — most
// components, Foundation among them, put their winmds directly in metadata/.
func chooseTargetPlatform(winmds map[string][]byte) (map[string][]byte, []string) {
	type candidate struct {
		entry   string
		version string
	}
	best := map[string]candidate{}
	passthrough := map[string][]byte{}

	for entry := range winmds {
		clean := strings.ReplaceAll(entry, "\\", "/")
		base := path.Base(clean)
		version := path.Base(path.Dir(clean))
		if !targetPlatformDir.MatchString(version) {
			passthrough[entry] = winmds[entry]
			continue
		}
		if current, seen := best[base]; !seen || compareVersions(version, current.version) > 0 {
			best[base] = candidate{entry: entry, version: version}
		}
	}

	var notes []string
	for base, winner := range best {
		passthrough[winner.entry] = winmds[winner.entry]
		var others []string
		for entry := range winmds {
			clean := strings.ReplaceAll(entry, "\\", "/")
			if path.Base(clean) == base && entry != winner.entry {
				others = append(others, path.Base(path.Dir(clean)))
			}
		}
		if len(others) > 0 {
			sort.Strings(others)
			notes = append(notes, fmt.Sprintf("%s: kept %s, dropped %s",
				base, winner.version, strings.Join(others, ", ")))
		}
	}
	sort.Strings(notes)
	return passthrough, notes
}

// compareVersions orders dotted numeric versions. Unparsable segments compare
// as zero, which is good enough: these come from directory names the packages
// themselves generate.
func compareVersions(a, b string) int {
	aParts, bParts := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(aParts) || i < len(bParts); i++ {
		var x, y int
		if i < len(aParts) {
			x, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			y, _ = strconv.Atoi(bParts[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// sortedKeys returns a map's keys in sorted order. Every stage iterates maps
// somewhere, and unsorted iteration is the commonest source of output that
// differs between runs — which the committed metadata cannot tolerate.
func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
