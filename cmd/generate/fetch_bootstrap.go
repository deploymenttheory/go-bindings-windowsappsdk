package main

// The fetch-bootstrap stage: download the Windows App SDK bootstrapper and the
// headers its version constants come from.
//
// An application that is not packaged with MSIX cannot activate any
// Microsoft.UI.* type until it has added the Windows App SDK framework
// package to its own process, which means calling MddBootstrapInitialize2 —
// exported from Microsoft.WindowsAppRuntime.Bootstrap.dll.
//
// That DLL is not on the machine. A recursive search of
// C:\Program Files\WindowsApps finds no file matching *Bootstrap* even when
// the framework packages are installed: it ships only in the NuGet package,
// for redistribution beside the executable. So it is fetched here and left
// out of version control (.gitignore excludes *.dll and /metadata/bootstrap/).
//
//	go run ./cmd/generate fetch-bootstrap [--version 2.3.1] [--arch x64] [--out metadata/bootstrap]

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/deploymenttheory/go-winmd/nuget"
)

const (
	// foundationPackage is the component holding the bootstrapper and headers.
	// The component that carries it is resolved from the meta-package's
	// dependency list rather than pinned here, because component versions
	// change with every servicing release.
	foundationPackage = "microsoft.windowsappsdk.foundation"

	bootstrapDLL = "Microsoft.WindowsAppRuntime.Bootstrap.dll"
)

func runFetchBootstrap(args []string) error {
	flags := flag.NewFlagSet("fetch-bootstrap", flag.ExitOnError)
	version := flags.String("version", pinnedSDKVersion, "Windows App SDK version")
	arch := flags.String("arch", "x64", "architecture: x64, x86, arm64")
	out := flags.String("out", filepath.Join("metadata", "bootstrap"), "output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return fetchBootstrap(*version, *arch, *out)
}

func fetchBootstrap(version, arch, out string) error {
	client := nuget.NewClient()

	// Resolve the Foundation component's version from the meta-package rather
	// than assuming it matches: in 2.3.1 the meta-package is 2.3.1 while
	// Foundation is 2.3.5.
	foundationVersion, err := resolveFoundation(client, version)
	if err != nil {
		return err
	}
	fmt.Printf("Windows App SDK %s → %s %s\n", version, foundationPackage, foundationVersion)

	nupkg, source, err := nuget.FetchArchive(client, foundationPackage, foundationVersion)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", foundationPackage, err)
	}
	fmt.Printf("  %d bytes from %s\n", len(nupkg), source)

	// The DLL lives under runtimes/win-<arch>/native/. Match on that suffix
	// rather than a whole path, because the layout has moved between releases
	// (lib/win10-x64 → lib/native/x64 → runtimes/win-<arch>/native).
	dllSuffix := path.Join("runtimes", "win-"+arch, "native", bootstrapDLL)
	wanted := func(name string) bool {
		clean := strings.ReplaceAll(name, "\\", "/")
		if strings.HasSuffix(clean, dllSuffix) {
			return true
		}
		base := path.Base(clean)
		return base == "MddBootstrap.h" || strings.HasSuffix(base, "VersionInfo.h")
	}

	found, err := nuget.ExtractMatching(nupkg, wanted)
	if err != nil {
		return fmt.Errorf("reading %s: %w", foundationPackage, err)
	}
	if len(found) == 0 {
		return fmt.Errorf("no bootstrapper or headers found for arch %q; "+
			"run `go run ./cmd/probe-nupkg --package %s --version %s` to see the layout",
			arch, foundationPackage, foundationVersion)
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var records []nuget.Provenance
	for name, content := range found {
		target := filepath.Join(out, path.Base(strings.ReplaceAll(name, "\\", "/")))
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return err
		}
		fmt.Printf("  wrote %-46s %8d bytes\n", target, len(content))
		records = append(records, nuget.ProvenanceFor(
			"Microsoft.WindowsAppSDK.Foundation", foundationVersion, source, name, content))
	}

	if err := nuget.WriteProvenance(filepath.Join(out, "PROVENANCE.json"), records); err != nil {
		return err
	}

	dllPath := filepath.Join(out, bootstrapDLL)
	if _, err := os.Stat(dllPath); err != nil {
		return fmt.Errorf("bootstrapper not among the extracted files (arch %q)", arch)
	}
	fmt.Printf("\nSet GOWINUI_BOOTSTRAP_DLL to load it explicitly:\n  %s\n", mustAbs(dllPath))
	return nil
}

// resolveFoundation reads the meta-package's dependency list and returns the
// concrete version of the Foundation component.
func resolveFoundation(client *http.Client, version string) (string, error) {
	dependencies, err := nuget.Dependencies(client, sdkPackage, version)
	if err != nil {
		return "", fmt.Errorf("reading the %s dependency list: %w", sdkPackage, err)
	}
	for _, dependency := range dependencies {
		if !strings.EqualFold(dependency.ID, "Microsoft.WindowsAppSDK.Foundation") {
			continue
		}
		resolved, err := dependency.Version()
		if err != nil {
			return "", err
		}
		return resolved, nil
	}
	return "", fmt.Errorf("%s %s does not depend on Microsoft.WindowsAppSDK.Foundation", sdkPackage, version)
}

func mustAbs(p string) string {
	absolute, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return absolute
}
