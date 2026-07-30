//go:build windows && amd64

package winui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	"github.com/deploymenttheory/go-bindings-win32/bindings/win32/foundation"
	"github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/libraryloader"
)

// Bootstrapping the Windows App SDK.
//
// Microsoft.UI.* runtime classes are not registered with the operating system
// the way Windows.* classes are, so RoGetActivationFactory cannot find them.
// An application without an MSIX package has to add the Windows App SDK
// framework package to its own process first, which is what
// MddBootstrapInitialize2 does.
//
// That function is exported from Microsoft.WindowsAppRuntime.Bootstrap.dll,
// which is NOT installed on the machine — a recursive search of
// C:\Program Files\WindowsApps finds no file matching *Bootstrap* even with
// the framework packages present. It ships only in the NuGet package and is
// meant to be redistributed beside the executable. Fetch it with
// `go run ./cmd/fetch-bootstrap`.

// PackageVersion is a Windows PACKAGE_VERSION: four 16-bit fields packed into
// one 64-bit value.
type PackageVersion uint64

// PackVersion builds a PackageVersion from its parts.
func PackVersion(major, minor, build, revision uint16) PackageVersion {
	return PackageVersion(major)<<48 | PackageVersion(minor)<<32 |
		PackageVersion(build)<<16 | PackageVersion(revision)
}

func (v PackageVersion) String() string {
	return fmt.Sprintf("%d.%d.%d.%d",
		uint16(v>>48), uint16(v>>32), uint16(v>>16), uint16(v))
}

// InitializeOptions mirrors MddBootstrapInitializeOptions in MddBootstrap.h.
type InitializeOptions uint32

const (
	// InitializeNone is the default behaviour.
	InitializeNone InitializeOptions = 0x0000
	// InitializeOnErrorDebugBreak calls DebugBreak() on failure.
	InitializeOnErrorDebugBreak InitializeOptions = 0x0001
	// InitializeOnErrorDebugBreakIfDebuggerAttached calls DebugBreak() on
	// failure, but only under a debugger.
	InitializeOnErrorDebugBreakIfDebuggerAttached InitializeOptions = 0x0002
	// InitializeOnErrorFailFast terminates the process on failure.
	InitializeOnErrorFailFast InitializeOptions = 0x0004
	// InitializeOnNoMatchShowUI offers to install the runtime if no matching
	// framework package is found.
	InitializeOnNoMatchShowUI InitializeOptions = 0x0008
	// InitializeOnPackageIdentityNoOp succeeds without doing anything when the
	// process already has package identity, instead of failing.
	InitializeOnPackageIdentityNoOp InitializeOptions = 0x0010
)

// MajorMinorVersion encodes the framework package version to request, as
// 0xMMMMNNNN.
//
// Note the asymmetry, which MddBootstrap.h is explicit about: the minor
// version is used only for the 1.x line, and ignored from 2.0 onward. So
// Windows App SDK 2.3 is requested as 0x00020000, not 0x00020003 — asking for
// the latter matches nothing.
type MajorMinorVersion uint32

const (
	// SDK2 requests the 2.x line, which is what this repository targets.
	SDK2 MajorMinorVersion = 0x00020000
	// SDK18 requests 1.8, the last of the 1.x line.
	SDK18 MajorMinorVersion = 0x00010008
)

// BootstrapOptions controls how the framework package is located.
type BootstrapOptions struct {
	// MajorMinor is the framework package version line to request.
	MajorMinor MajorMinorVersion
	// VersionTag is the pre-release identifier ("preview1"), empty for stable.
	VersionTag string
	// MinVersion is the minimum acceptable framework package version. The zero
	// value accepts any version in the requested line.
	MinVersion PackageVersion
	// Options is passed through to MddBootstrapInitialize2.
	Options InitializeOptions
	// DLLPath, when set, is the exact bootstrapper to load. When empty the
	// search order is: $GOWINUI_BOOTSTRAP_DLL, then the directory holding the
	// running executable, then metadata/bootstrap relative to the working
	// directory (where cmd/fetch-bootstrap puts it).
	DLLPath string
}

// DefaultBootstrap requests the Windows App SDK line this repository targets.
func DefaultBootstrap() BootstrapOptions {
	return BootstrapOptions{MajorMinor: SDK2}
}

// ErrBootstrapDLLNotFound reports that the redistributable bootstrapper could
// not be located. It lists where it looked, because the fix is always to put
// the file in one of those places.
type ErrBootstrapDLLNotFound struct {
	Searched []string
}

func (e *ErrBootstrapDLLNotFound) Error() string {
	return fmt.Sprintf("winui: %s not found (searched: %v); "+
		"fetch it with `go run ./cmd/fetch-bootstrap`, or set GOWINUI_BOOTSTRAP_DLL",
		bootstrapDLLName, e.Searched)
}

const bootstrapDLLName = "Microsoft.WindowsAppRuntime.Bootstrap.dll"

// hrNoMatchingFrameworkPackage is what MddBootstrapInitialize2 returns when the
// bootstrapper loaded and ran but found no installed framework package matching
// the requested version line: "Package dependency criteria could not be
// resolved."
//
// It is worth naming because it is the one failure with a completely different
// cause from the rest. Nothing is wrong with the call, the DLL or the arguments
// — the machine simply does not have the Windows App SDK runtime installed, and
// no amount of reading this package's code reveals that.
const hrNoMatchingFrameworkPackage = 0x80670016

// ErrNoMatchingFrameworkPackage reports that no installed Windows App SDK
// framework package matches the requested version.
type ErrNoMatchingFrameworkPackage struct {
	// MajorMinor is the version line that was asked for.
	MajorMinor MajorMinorVersion
	// MinVersion is the minimum acceptable framework package version.
	MinVersion PackageVersion
}

func (e *ErrNoMatchingFrameworkPackage) Error() string {
	return fmt.Sprintf("winui: no installed Windows App SDK framework package matches %#08x "+
		"(minimum %s); install the runtime from "+
		"https://aka.ms/windowsappsdk/2.3/latest/windowsappruntimeinstall-x64.exe",
		uint32(e.MajorMinor), e.MinVersion)
}

var (
	bootstrapMu       sync.Mutex
	bootstrapModule   foundation.HMODULE
	procInitialize2   foundation.FARPROC
	procShutdown      foundation.FARPROC
	bootstrapped      bool
	bootstrapResolved string
)

// Bootstrap adds the Windows App SDK framework package to this process, which
// every Microsoft.UI.* activation depends on. It is idempotent.
//
// COM must already be initialized on the calling thread. Callers driving a UI
// should enter their single-threaded apartment first.
func Bootstrap(options BootstrapOptions) error {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()
	if bootstrapped {
		return nil
	}
	if err := loadBootstrapDLL(options.DLLPath); err != nil {
		return err
	}

	var tag *uint16
	if options.VersionTag != "" {
		encoded, err := syscall.UTF16PtrFromString(options.VersionTag)
		if err != nil {
			return fmt.Errorf("winui: version tag %q: %w", options.VersionTag, err)
		}
		tag = encoded
	}

	majorMinor := options.MajorMinor
	if majorMinor == 0 {
		majorMinor = SDK2
	}

	result, _, _ := syscall.SyscallN(uintptr(procInitialize2),
		uintptr(majorMinor),
		uintptr(unsafe.Pointer(tag)),
		uintptr(options.MinVersion),
		uintptr(options.Options),
	)
	if err := win32.ErrIfFailed(int32(result)); err != nil {
		if uint32(result) == hrNoMatchingFrameworkPackage {
			return &ErrNoMatchingFrameworkPackage{MajorMinor: majorMinor, MinVersion: options.MinVersion}
		}
		return fmt.Errorf("winui: MddBootstrapInitialize2(%#08x, %q, %s): %w",
			uint32(majorMinor), options.VersionTag, options.MinVersion, err)
	}
	bootstrapped = true
	return nil
}

// BootstrapShutdown removes the framework package from this process. After it
// returns, no Windows App SDK API may be called.
func BootstrapShutdown() {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()
	if !bootstrapped || procShutdown == 0 {
		return
	}
	syscall.SyscallN(uintptr(procShutdown))
	bootstrapped = false
}

// Bootstrapped reports whether the framework package has been added.
func Bootstrapped() bool {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()
	return bootstrapped
}

// BootstrapDLLPath returns the bootstrapper that was loaded, once Bootstrap
// has succeeded. Useful in diagnostics, where "which copy did it load" is
// usually the question.
func BootstrapDLLPath() string {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()
	return bootstrapResolved
}

// loadBootstrapDLL loads the bootstrapper from an explicit, absolute path.
//
// It deliberately does NOT use go-bindings-win32's runtime loader, which
// searches System32 only as a defence against DLL preloading. This DLL is
// application-local by design — the Windows App SDK ships it for exactly that
// — so it can never be found there. The mitigation kept here is that the path
// is always resolved to an absolute one before loading, and never taken from
// the process search path.
func loadBootstrapDLL(explicit string) error {
	if bootstrapModule != 0 {
		return nil
	}
	absolute, err := findBootstrapDLL(explicit)
	if err != nil {
		return err
	}
	// LOAD_WITH_ALTERED_SEARCH_PATH makes the DLL's own directory the starting
	// point for resolving its dependencies, which is what an
	// application-local component expects.
	module, err := libraryloader.LoadLibraryEx(absolute,
		libraryloader.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return fmt.Errorf("winui: loading %s: %w", absolute, err)
	}
	if err := resolveBootstrapProcs(module); err != nil {
		return err
	}
	bootstrapModule = module
	bootstrapResolved = absolute
	return nil
}

// findBootstrapDLL resolves the first candidate that exists, as an absolute
// path. It is kept separate from loading so the search — and its failure —
// can be exercised without touching process-wide state.
func findBootstrapDLL(explicit string) (string, error) {
	candidates, err := bootstrapCandidates(explicit)
	if err != nil {
		return "", err
	}
	var searched []string
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		searched = append(searched, absolute)
		if _, err := os.Stat(absolute); err == nil {
			return absolute, nil
		}
	}
	return "", &ErrBootstrapDLLNotFound{Searched: searched}
}

// bootstrapCandidates lists where the bootstrapper may be, most explicit
// first.
func bootstrapCandidates(explicit string) ([]string, error) {
	if explicit != "" {
		return []string{explicit}, nil
	}
	var candidates []string
	if fromEnv := os.Getenv("GOWINUI_BOOTSTRAP_DLL"); fromEnv != "" {
		candidates = append(candidates, fromEnv)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), bootstrapDLLName))
	}
	// Where cmd/fetch-bootstrap puts it, for `go test` and `go run` where the
	// executable lives in a temporary directory.
	candidates = append(candidates,
		filepath.Join("metadata", "bootstrap", bootstrapDLLName),
		filepath.Join("..", "..", "..", "metadata", "bootstrap", bootstrapDLLName),
	)
	return candidates, nil
}

func resolveBootstrapProcs(module foundation.HMODULE) error {
	initialize, err := procAddress(module, "MddBootstrapInitialize2")
	if err != nil {
		return err
	}
	shutdown, err := procAddress(module, "MddBootstrapShutdown")
	if err != nil {
		return err
	}
	procInitialize2, procShutdown = initialize, shutdown
	return nil
}

func procAddress(module foundation.HMODULE, name string) (foundation.FARPROC, error) {
	encoded, err := syscall.BytePtrFromString(name)
	if err != nil {
		return 0, err
	}
	address, err := libraryloader.GetProcAddress(module, foundation.PSTR(encoded))
	if err != nil || address == 0 {
		if err == nil {
			err = errors.New("not exported")
		}
		return 0, fmt.Errorf("winui: %s in %s: %w", name, bootstrapDLLName, err)
	}
	return address, nil
}
