//go:build windows && amd64

package app

import (
	"errors"
	"testing"
)

// These cover the parts of the ergonomic layer that are pure control flow. The parts
// that touch WinRT — Box, On, Append, SetContent — are exercised against live objects
// in acceptance/, because what matters about them is reference counting, and a fake
// cannot get that wrong in the same way the real thing can.

// fakeInterface stands in for a generated interface pointer, counting its releases.
type fakeInterface struct{ released int }

func (f *fakeInterface) Release() uint32 { f.released++; return 0 }

func TestAllReturnsTheFirstError(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")

	if err := All(nil, nil, nil); err != nil {
		t.Errorf("All of nils = %v, want nil", err)
	}
	if err := All(); err != nil {
		t.Errorf("All of nothing = %v, want nil", err)
	}
	if err := All(nil, first, nil, second); !errors.Is(err, first) {
		t.Errorf("All = %v, want the first error", err)
	}
}

// TestAllEvaluatesEveryArgument is the property that makes All usable for setters at
// all. They are calls, not values: if Go could skip the later ones the block would
// stop halfway and the caller would never know which properties were applied.
//
// This is a language guarantee rather than something All implements — arguments are
// evaluated before the call — so the test is here to state the dependency, not to
// check the implementation.
func TestAllEvaluatesEveryArgument(t *testing.T) {
	var calls int
	step := func(err error) error { calls++; return err }

	_ = All(step(nil), step(errors.New("boom")), step(nil))
	if calls != 3 {
		t.Errorf("%d of 3 arguments evaluated; a setter block would stop halfway", calls)
	}
}

func TestWithReleasesWhateverHappens(t *testing.T) {
	// The ordinary path.
	value := &fakeInterface{}
	if err := With(func() (*fakeInterface, error) { return value, nil },
		func(*fakeInterface) error { return nil }); err != nil {
		t.Fatalf("With: %v", err)
	}
	if value.released != 1 {
		t.Errorf("released %d times, want 1", value.released)
	}

	// fn fails: the release still has to happen, which is the whole reason this helper
	// exists rather than three lines at each call site.
	failed := &fakeInterface{}
	boom := errors.New("boom")
	if err := With(func() (*fakeInterface, error) { return failed, nil },
		func(*fakeInterface) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("With = %v, want fn's error", err)
	}
	if failed.released != 1 {
		t.Error("a failing body leaked the interface")
	}
}

func TestWithDoesNotCallTheBodyWhenTheQueryFails(t *testing.T) {
	boom := errors.New("no such interface")
	called := false
	err := With(func() (*fakeInterface, error) { return nil, boom },
		func(*fakeInterface) error { called = true; return nil })
	if !errors.Is(err, boom) {
		t.Errorf("With = %v, want the query's error", err)
	}
	if called {
		t.Error("the body ran against an interface that was never obtained")
	}
}

// TestBoxRefusesWhatItCannotRepresent pins the choice to fail rather than fall back to
// CreateEmpty. A Content that silently became empty is harder to debug than one that
// refused, because nothing in the UI distinguishes it from a Content never set.
//
// The unsupported type is checked before any activation, so this needs no apartment.
func TestBoxRefusesWhatItCannotRepresent(t *testing.T) {
	type notABoxableThing struct{ x int }
	if _, err := Box(notABoxableThing{}); err == nil {
		t.Error("Box accepted a type WinRT has no property value for")
	} else if got := err.Error(); got == "" {
		t.Error("Box's refusal does not say what it could not box")
	}
}
