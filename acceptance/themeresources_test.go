//go:build windows && amd64

package acceptance

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// Some controls cannot be created at all, and kill the process when one is laid out.
//
// TextBox, PasswordBox, RichEditBox, SearchBox and ProgressBar die with 0xC000027B — a
// stowed WinRT exception — when the element is laid out. TextBlock, Button, CheckBox,
// Slider, ListView, ComboBox, ScrollViewer and Grid are fine in the same harness.
//
// The cause is NOT in this projection, and that is established rather than assumed:
//
//	XamlReader.Load("<Button/>")   -> ok
//	XamlReader.Load("<TextBox/>")  -> HRESULT 0x802B000A
//
// The second is the framework's own parser building its own object from markup, with
// nothing of ours involved, and 0x802B000A is the inner HRESULT of the crash — facility
// 0x2B is XAML. Windows Error Reporting names the faulting module as
// Microsoft.UI.Xaml.dll.
//
// What it is: those controls' default styles are not reachable. ms-appx:/// does not
// resolve in this process, so nothing that needs the theme dictionaries can load —
//
//	ResourceDictionary.SetSource("ms-appx:///Microsoft.UI.Xaml/Themes/generic.xaml") -> E_FAIL
//	NewXamlControlsResources()                                                       -> E_FAIL
//	XamlReader.Load("<XamlControlsResources/>")                                      -> 0x802B000A
//
// The framework package ships those resources in Microsoft.UI.Xaml.Controls.pri, whose
// resource map is named Microsoft.UI.Xaml and which holds
// Files/Microsoft.UI.Xaml/Themes/generic.xbf. An unpackaged application resolves
// ms-appx:/// through its OWN resources.pri, and a `go build` produces none — which is
// the gap. MSBuild closes it for C# apps by MERGING the framework's PRI into one it
// generates for the application.
//
// Copying that PRI beside the executable does not work, as either resources.pri or with
// the executable renamed to match the resource map. So the fix is a real makepri merge
// rather than a file copy, which makes it build tooling this repository does not have
// yet. That is the open work; this test is the reproduction.
//
// The earlier note in CLAUDE.md — that XamlControlsResources failing "does not matter" —
// was reached by measuring a Button, and holds only for the controls it was measured on.
//
// The test runs the case in a SUBPROCESS, because the failure is a process death and
// nothing in Go recovers from it.

// themeResourceSubprocessEnv, when set, makes the test binary build a window containing the
// named control and wait for layout, rather than run the suite.
const themeResourceSubprocessEnv = "WASDK_THEME_RESOURCE_CASE"

// TestMain lets the test binary re-enter as the crashing child.
func TestMain(m *testing.M) {
	if control := os.Getenv(themeResourceSubprocessEnv); control != "" {
		os.Exit(runThemeResourceCase(control))
	}
	os.Exit(m.Run())
}

// runThemeResourceCase puts one control in a window and waits for it to lay out. It returns
// 0 if Loaded fires, and does not return at all if the framework kills the process.
func runThemeResourceCase(control string) int {
	err := app.Run(func(ready *app.Ready) error {
		panel, err := uixaml.NewStackPanel()
		if err != nil {
			return err
		}
		var child func() (*uixaml.IUIElement, error)
		switch control {
		case "TextBox":
			c, err := uixaml.NewTextBox()
			if err != nil {
				return err
			}
			child = c.AsUIElement
		case "PasswordBox":
			c, err := uixaml.NewPasswordBox()
			if err != nil {
				return err
			}
			child = c.AsUIElement
		case "ProgressBar":
			c, err := uixaml.NewProgressBar()
			if err != nil {
				return err
			}
			child = c.AsUIElement
		case "TextBlock":
			c, err := uixaml.NewTextBlock()
			if err != nil {
				return err
			}
			if err := c.SetText("control case"); err != nil {
				return err
			}
			child = c.AsUIElement
		default:
			return nil
		}
		if err := app.Append(panel.AsPanel, child); err != nil {
			return err
		}
		loaded, err := uixaml.NewRoutedEventHandler(
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				_ = ready.Application.Exit()
			})
		if err != nil {
			return err
		}
		if err := app.With(panel.AsFrameworkElement, func(fe *uixaml.IFrameworkElement) error {
			_, addErr := fe.AddLoaded(loaded)
			return addErr
		}); err != nil {
			return err
		}
		if err := app.With(panel.AsUIElement, ready.Window.SetContent); err != nil {
			return err
		}
		return ready.Window.Activate()
	}, app.Options{})
	if err != nil {
		return 2
	}
	return 0
}

// TestControlsNeedingThemeResourcesCannotLoad pins the limitation, and pins a control
// case beside it.
//
// TextBlock is not padding. Without it, a harness that was simply broken — a bad
// subprocess invocation, a missing runtime — would produce the same failures and read
// as a discovery about the controls.
//
// ProgressBar is in the list deliberately. The first description of this was "text
// input controls crash", which was wrong: ProgressBar carries no text, and
// AutoSuggestBox — which does — parses fine on its own. Keeping ProgressBar here stops
// that description coming back.
func TestControlsNeedingThemeResourcesCannotLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocesses that are expected to die")
	}
	for _, testCase := range []struct {
		control   string
		wantCrash bool
	}{
		{"TextBlock", false},
		{"TextBox", true},
		{"PasswordBox", true},
		{"ProgressBar", true},
	} {
		output, err := runChild(t, testCase.control)
		crashed := err != nil

		switch {
		case testCase.wantCrash && !crashed:
			t.Errorf("%s laid out without dying — the theme resources now load, so this "+
				"test and the notes in CLAUDE.md and README.md should go", testCase.control)
		case !testCase.wantCrash && crashed:
			// The control case failed, so nothing above it means anything.
			if strings.Contains(output, "bootstrapper") || strings.Contains(output, "framework package") {
				t.Skipf("%s: the Windows App SDK runtime is unavailable: %v", testCase.control, err)
			}
			t.Fatalf("%s died too, so the harness is broken rather than text input: %v\n%s",
				testCase.control, err, output)
		case testCase.wantCrash && crashed:
			t.Logf("%s: %v (expected; see this file's comment)", testCase.control, err)
		}
	}
}

// runChild re-executes this test binary with the subprocess environment set.
func runChild(t *testing.T, control string) (string, error) {
	t.Helper()
	command := exec.Command(os.Args[0])
	command.Env = append(os.Environ(), themeResourceSubprocessEnv+"="+control)
	output, err := command.CombinedOutput()
	return string(output), err
}
