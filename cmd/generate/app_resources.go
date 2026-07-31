package main

// app-resources builds the resources.pri that an unpackaged Windows App SDK
// application needs beside its executable.
//
// WinUI's controls do not carry their default styles in the DLL. They live in
// Microsoft.UI.Xaml.Controls.pri inside the installed framework package, as compiled
// XBF embedded in that index, and XAML reaches them through ms-appx:/// URIs. An
// unpackaged application resolves ms-appx:/// against its OWN resources.pri, and
// `go build` produces none — so every lookup fails, and the controls whose templates
// depend on one cannot be created.
//
// MSBuild does this for C# applications and never mentions it. There is no equivalent
// for a Go build, which is why it is here.
//
// IMPORTANT, and stated plainly because the alternative is a user losing an afternoon:
// this file is NECESSARY BUT NOT SUFFICIENT ON ITS OWN. It settles where the theme
// dictionaries live; it does not settle who resolves the type names inside them. WinUI
// asks the application object for IXamlMetadataProvider, and an application that
// activated Microsoft.UI.Xaml.Application directly cannot answer — so with this file
// in place and nothing else, ProgressBar still fails with "The type 'ProgressBar' was
// not found".
//
// app.Run builds an application that answers, so a caller using it needs only this file.
// acceptance/themeresources_test.go asserts both halves: the resource lookup that this
// command fixes, and the type resolution that the derived application fixes.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// frameworkPackagePrefix is how the installed Windows App SDK framework package
// directory begins. The version and architecture vary, so the newest match wins.
const frameworkPackagePrefix = "Microsoft.WindowsAppRuntime."

// controlsResourceIndex is the framework's own index, carrying the XAML theme
// dictionaries as embedded XBF.
const controlsResourceIndex = "Microsoft.UI.Xaml.Controls.pri"

func runAppResources(args []string) error {
	flags := flag.NewFlagSet("app-resources", flag.ExitOnError)
	out := flags.String("out", ".", "directory to write resources.pri into (the one holding the executable)")
	name := flags.String("name", "", "resource map name; must match the executable's base name (default: inferred from --out)")
	keep := flags.Bool("keep-language-splits", false, "keep the per-language resources.language-*.pri files")
	if err := flags.Parse(args); err != nil {
		return err
	}

	makepri, err := findMakepri()
	if err != nil {
		return err
	}
	framework, err := findFrameworkPackage()
	if err != nil {
		return err
	}
	source := filepath.Join(framework, controlsResourceIndex)
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("app-resources: %s is not in the framework package at %s: %w",
			controlsResourceIndex, framework, err)
	}

	outDir := mustAbs(*out)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	mapName := *name
	if mapName == "" {
		mapName = filepath.Base(outDir)
	}

	// makepri indexes a DIRECTORY, so the framework's index is staged into a scratch
	// one. Indexing the output directory directly would sweep the executable and
	// anything else beside it into the resource map.
	staging, err := os.MkdirTemp("", "wasdk-pri-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyFile(source, filepath.Join(staging, controlsResourceIndex)); err != nil {
		return err
	}

	config := filepath.Join(staging, "priconfig.xml")
	if err := run(makepri, "createconfig", "/cf", config, "/dq", "en-US", "/o"); err != nil {
		return fmt.Errorf("app-resources: makepri createconfig: %w", err)
	}
	target := filepath.Join(outDir, "resources.pri")
	if err := run(makepri, "new", "/pr", staging, "/cf", config, "/of", target, "/in", mapName, "/o"); err != nil {
		return fmt.Errorf("app-resources: makepri new: %w", err)
	}

	// The default config splits by language, which for a Go application is 80 files
	// nobody asked for. The main index carries en-US and is self-sufficient.
	removed := 0
	if !*keep {
		matches, _ := filepath.Glob(filepath.Join(outDir, "resources.language-*.pri"))
		for _, path := range matches {
			if os.Remove(path) == nil {
				removed++
			}
		}
	}

	fmt.Printf("wrote %s (resource map %q, from %s)\n", target, mapName, filepath.Base(framework))
	if removed > 0 {
		fmt.Printf("removed %d per-language index files (--keep-language-splits to retain them)\n", removed)
	}
	fmt.Println("NOTE: this file is one of two things the theme dictionaries need. The other is")
	fmt.Println("an application that answers IXamlMetadataProvider, which app.Run builds for you.")
	fmt.Println("An application activating Microsoft.UI.Xaml.Application directly has a")
	fmt.Println("resources.pri and still cannot load them. See acceptance/themeresources_test.go.")
	return nil
}

// findMakepri locates makepri.exe in an installed Windows SDK, newest version first.
// It is not redistributable, so its absence is a clear message rather than a fallback.
func findMakepri() (string, error) {
	var roots []string
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if base == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(base, "Windows Kits", "10", "bin", "*", "x64", "makepri.exe"))
		roots = append(roots, matches...)
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("app-resources: makepri.exe not found; install the Windows SDK " +
			"(it ships in Windows Kits\\10\\bin\\<version>\\x64) — it is not redistributable, " +
			"so this repository cannot vendor it")
	}
	sort.Strings(roots)
	return roots[len(roots)-1], nil
}

// findFrameworkPackage locates the installed Windows App SDK framework package.
//
// The package manager is asked rather than C:\Program Files\WindowsApps being listed,
// because that directory denies enumeration: a known path inside it can be read, but
// ReadDir returns "Access is denied". Get-AppxPackage is the supported route and needs
// no elevation.
//
// Selection is by the MAJOR VERSION IN THE NAME, not by the Version field, and that
// distinction is the whole reason this is not one line. Microsoft.WindowsAppRuntime.1.8
// carries version 8000.921.1539.0 while Microsoft.WindowsAppRuntime.2 carries 2.3.1.0,
// so sorting on Version picks the OLDER SDK. The .CBS. variants are excluded: they are
// the servicing singletons, not the framework an application binds to.
func findFrameworkPackage() (string, error) {
	const script = `Get-AppxPackage -Name 'Microsoft.WindowsAppRuntime.*' | ` +
		`Where-Object { $_.Architecture -eq 'X64' -and $_.Name -notlike '*.CBS.*' } | ` +
		`ForEach-Object { '{0}|{1}' -f $_.Name, $_.InstallLocation }`

	output, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("app-resources: asking the package manager for the framework package: %w", err)
	}

	var candidates []frameworkCandidate
	for _, line := range strings.Split(string(output), "\n") {
		name, location, found := strings.Cut(strings.TrimSpace(line), "|")
		if !found || location == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(location, controlsResourceIndex)); err != nil {
			continue // a package without the XAML resources is not the one wanted
		}
		candidates = append(candidates, frameworkCandidate{Name: name, Location: location})
	}
	best := selectFramework(candidates)
	if best == "" {
		return "", fmt.Errorf("app-resources: no Windows App SDK framework package with %s is installed; "+
			"install the Windows App SDK runtime", controlsResourceIndex)
	}
	return best, nil
}

func run(name string, args ...string) error {
	command := exec.Command(name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", filepath.Base(name), err, output)
	}
	return nil
}

func copyFile(from, to string) error {
	content, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, content, 0o644)
}

// frameworkCandidate is one installed framework package.
type frameworkCandidate struct {
	Name     string // "Microsoft.WindowsAppRuntime.2"
	Location string // its install directory
}

// selectFramework picks the newest framework package by the MAJOR VERSION IN ITS NAME.
//
// Separated from the package-manager call so the rule can be tested without an
// installed SDK, because the rule is the part that is easy to get wrong: sorting on the
// package Version field picks Microsoft.WindowsAppRuntime.1.8, whose version is
// 8000.921.1539.0, over Microsoft.WindowsAppRuntime.2, whose version is 2.3.1.0.
func selectFramework(candidates []frameworkCandidate) string {
	best, bestMajor := "", -1
	for _, candidate := range candidates {
		suffix := strings.TrimPrefix(candidate.Name, frameworkPackagePrefix)
		major, _, _ := strings.Cut(suffix, ".")
		value := 0
		if _, err := fmt.Sscanf(major, "%d", &value); err != nil {
			continue
		}
		if value > bestMajor {
			best, bestMajor = candidate.Location, value
		}
	}
	return best
}
