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

// Every text-input control terminates the process during layout.
//
// TextBox, PasswordBox, RichEditBox and AutoSuggestBox all die with 0xC000027B, a
// stowed WinRT exception, at the point the element is laid out. TextBlock, Button,
// CheckBox, Slider, ListView and Grid in the same harness are fine, so this is not the
// window, the bootstrap, or the projection generally.
//
// It is NOT a projection bug in any way that is visible from Go: the constructor
// succeeds, properties set without error, and the element goes into the tree. The
// process dies later, inside the framework, once layout runs — which is why the
// existing acceptance tests never saw it. They measure at Loaded, and a TextBox never
// reaches Loaded.
//
// The cause is not established. It is recorded here rather than explained, because the
// last two times something in this repository was explained without a discriminator the
// explanation was wrong. What is known:
//
//   - it is specific to text INPUT; TextBlock renders text fine
//   - it is not construction or property access, both of which succeed
//   - it survives no styling workaround tried so far
//
// The obvious suspect is the XamlControlsResources failure documented in CLAUDE.md,
// whose conclusion — "it does not matter" — was reached by measuring a Button. That
// conclusion may be true only of the controls it was measured on. Establishing that
// needs a discriminator, not a guess.
//
// The test runs the case in a SUBPROCESS, because the failure is a process death and
// nothing in Go recovers from it.

// textInputSubprocessEnv, when set, makes the test binary build a window containing the
// named control and wait for layout, rather than run the suite.
const textInputSubprocessEnv = "WASDK_TEXT_INPUT_CASE"

// TestMain lets the test binary re-enter as the crashing child.
func TestMain(m *testing.M) {
	if control := os.Getenv(textInputSubprocessEnv); control != "" {
		os.Exit(runTextInputCase(control))
	}
	os.Exit(m.Run())
}

// runTextInputCase puts one control in a window and waits for it to lay out. It returns
// 0 if Loaded fires, and does not return at all if the framework kills the process.
func runTextInputCase(control string) int {
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

// TestTextInputControlsCrashDuringLayout pins the limitation, and pins the control case
// beside it.
//
// TextBlock is not padding. Without it, a harness that was simply broken — a bad
// subprocess invocation, a missing runtime — would produce the same failures and read
// as a discovery about text input.
func TestTextInputControlsCrashDuringLayout(t *testing.T) {
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
	} {
		output, err := runChild(t, testCase.control)
		crashed := err != nil

		switch {
		case testCase.wantCrash && !crashed:
			t.Errorf("%s laid out without dying — the limitation this test tracks is "+
				"fixed, and both this test and the notes in CLAUDE.md should go", testCase.control)
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
	command.Env = append(os.Environ(), textInputSubprocessEnv+"="+control)
	output, err := command.CombinedOutput()
	return string(output), err
}
