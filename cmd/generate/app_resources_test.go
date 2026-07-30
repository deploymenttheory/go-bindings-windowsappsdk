package main

import "testing"

// TestSelectFrameworkPrefersTheNameMajor pins the one rule here that is easy to get
// backwards.
//
// The package Version field does not order these SDKs:
// Microsoft.WindowsAppRuntime.1.8 carries 8000.921.1539.0 and
// Microsoft.WindowsAppRuntime.2 carries 2.3.1.0, so sorting on Version selects the
// OLDER SDK and its older theme resources. The major in the NAME is the ordering.
func TestSelectFrameworkPrefersTheNameMajor(t *testing.T) {
	installed := []frameworkCandidate{
		{"Microsoft.WindowsAppRuntime.1.8", `C:\WindowsApps\wart-1.8`},
		{"Microsoft.WindowsAppRuntime.2", `C:\WindowsApps\wart-2`},
	}
	if got := selectFramework(installed); got != `C:\WindowsApps\wart-2` {
		t.Errorf("selectFramework = %q, want the major-2 package", got)
	}
	// Order of discovery must not matter.
	reversed := []frameworkCandidate{installed[1], installed[0]}
	if got := selectFramework(reversed); got != `C:\WindowsApps\wart-2` {
		t.Errorf("selectFramework (reversed) = %q, want the major-2 package", got)
	}
}

func TestSelectFrameworkHandlesNothingUsable(t *testing.T) {
	if got := selectFramework(nil); got != "" {
		t.Errorf("selectFramework(nil) = %q, want empty", got)
	}
	// A name that does not carry a parsable major is skipped rather than chosen.
	if got := selectFramework([]frameworkCandidate{{"Microsoft.WindowsAppRuntime.Preview", `C:\x`}}); got != "" {
		t.Errorf("selectFramework = %q, want empty for an unparsable major", got)
	}
}
