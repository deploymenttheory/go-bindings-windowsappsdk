//go:build windows && amd64

package acceptance

// The gallery conformance suite.
//
// Every page registered in examples/gallery/pages is built, put in a window, and
// asserted to lay out. The registry is the single source: this iterates it rather than
// naming pages of its own, so a page cannot be added without also being checked.
//
// Why layout and not merely construction: the control crash that made a third of the
// WinUI set unusable was invisible until layout ran. Constructors succeeded, properties
// set without error, the element went into the tree, and the process died afterwards.
// A suite that only built controls would have reported everything fine.
//
// Runs in a SUBPROCESS staged with a resources.pri, for the two reasons
// themeresources_test.go documents: a page that kills the process must name itself
// rather than take the suite down, and ms-appx:/// resolves against the running
// executable's own resource index, which a `go test` binary in a temp directory has not
// got.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/examples/gallery/pages"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// galleryPageEnv, when set, makes the test binary build that one page and exit rather
// than run the suite.
const galleryPageEnv = "WASDK_GALLERY_PAGE"

// runGalleryPage builds one page, waits for it to lay out, and reports whether it did.
// Returns 0 on success; does not return at all if the framework kills the process.
func runGalleryPage(key string) int {
	page, err := pages.Lookup(key)
	if err != nil || page.Build == nil {
		return 3
	}

	var laidOut bool
	runErr := app.Run(func(ready *app.Ready) error {
		element, err := page.Build(ready)
		if err != nil {
			return err
		}
		defer element.Release()

		// Loaded is the earliest point at which a size means anything: the element is in
		// the live tree with its template applied.
		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				laidOut = true
				_ = ready.Application.Exit()
			})
		if err != nil {
			return err
		}
		// A page hands back IUIElement. Loaded is on IFrameworkElement, and the two are
		// related by class inheritance rather than by a Requires clause — so there is no
		// generated accessor between them and this is a plain query.
		frame, err := winrt.QueryInterface[uixaml.IFrameworkElement](
			unsafe.Pointer(element), &uixaml.IID_IFrameworkElement)
		if err != nil {
			return err
		}
		defer frame.Release()
		if _, err := frame.AddLoaded(loaded); err != nil {
			return err
		}
		if err := ready.Window.SetContent(element); err != nil {
			return err
		}
		return ready.Window.Activate()
	}, app.Options{})

	switch {
	case runErr != nil:
		// Printed, not swallowed. Without this a page that fails to build reports only
		// "exit status 2" to the parent, which says nothing about why — and the whole
		// point of a conformance suite is that the failure names itself.
		fmt.Fprintln(os.Stderr, "gallery:", key, "failed to build:", runErr)
		return 2
	case !laidOut:
		fmt.Fprintln(os.Stderr, "gallery:", key, "built but never fired Loaded")
		return 4
	}
	return 0
}

// TestGalleryPagesLayOut is the suite.
func TestGalleryPagesLayOut(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a resources.pri and spawns a subprocess per page")
	}
	buildable := pages.Buildable()
	if len(buildable) == 0 {
		t.Fatal("no pages are registered, so this suite asserts nothing")
	}
	staging := stageGallerySubprocess(t)

	for _, page := range buildable {
		key := page.Key()
		t.Run(key, func(t *testing.T) {
			output, err, timedOut := runGalleryChild(t, staging, key)
			switch {
			case err == nil:
				return
			case timedOut:
				t.Errorf("%s hung", key)
			case strings.Contains(output, "bootstrapper") || strings.Contains(output, "framework package"):
				t.Skipf("the Windows App SDK runtime is unavailable: %v", err)
			default:
				t.Errorf("%s did not lay out: %v\n%s", key, err, output)
			}
		})
	}

	// Pages recorded as unmappable must say why. A page with neither a Build nor a
	// reason is one nobody has reached yet, which the registry refuses at registration —
	// this checks the reason is a reason.
	for _, page := range pages.Pages() {
		if page.Build == nil && len(page.Unmappable) < 10 {
			t.Errorf("%s is unmappable but gives no useful reason: %q", page.Key(), page.Unmappable)
		}
	}
}

// stageGallerySubprocess prepares a directory the child can run from.
func stageGallerySubprocess(t *testing.T) string {
	t.Helper()
	staging := t.TempDir()
	const name = "gallery.test.exe"

	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Skipf("cannot read this test binary to stage a subprocess: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, name), binary, 0o755); err != nil {
		t.Fatalf("staging the test binary: %v", err)
	}
	bootstrapper := filepath.Join("..", "metadata", "bootstrap", "Microsoft.WindowsAppRuntime.Bootstrap.dll")
	dll, err := os.ReadFile(bootstrapper)
	if err != nil {
		t.Skipf("bootstrapper not fetched; run `go run ./cmd/generate fetch-bootstrap`: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, filepath.Base(bootstrapper)), dll, 0o755); err != nil {
		t.Fatalf("staging the bootstrapper: %v", err)
	}
	generate := exec.Command("go", "run", "../cmd/generate", "app-resources",
		"--out", staging, "--name", strings.TrimSuffix(name, ".exe"))
	if output, err := generate.CombinedOutput(); err != nil {
		t.Skipf("could not build a resources.pri (needs the Windows SDK's makepri and an "+
			"installed Windows App SDK runtime): %v\n%s", err, output)
	}
	return staging
}

// runGalleryChild runs the staged binary against one page.
func runGalleryChild(t *testing.T, staging, key string) (output string, err error, timedOut bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, filepath.Join(staging, "gallery.test.exe"))
	command.Dir = staging
	command.Env = append(os.Environ(), galleryPageEnv+"="+key)
	raw, err := command.CombinedOutput()
	return string(raw), err, ctx.Err() != nil
}
