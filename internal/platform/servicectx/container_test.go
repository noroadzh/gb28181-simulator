package servicectx_test

import (
	"errors"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/platform/servicectx"
)

// fakeCloser tracks close order for reverse-shutdown assertions.
type fakeCloser struct {
	id    string
	order *[]string
}

func (f *fakeCloser) Close() error {
	*f.order = append(*f.order, f.id)
	return nil
}

// TestProvideBuildGet verifies the happy path: Provide in declaration
// order, Build resolves every provider, Get/MustGet return the same
// instance.
func TestProvideBuildGet(t *testing.T) {
	kA := servicectx.NewKey[int]("a")
	kB := servicectx.NewKey[string]("b")

	c := servicectx.NewContainer().
		Provide(kA, func() (any, error) { return 42, nil }).
		Provide(kB, func() (any, error) { return "hi", nil })

	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	if got, ok := servicectx.GetTyped[int](c, kA); !ok || got != 42 {
		t.Errorf("GetTyped(int)=%v,%v want 42,true", got, ok)
	}
	if got, ok := servicectx.GetTyped[string](c, kB); !ok || got != "hi" {
		t.Errorf("GetTyped(string)=%v,%v want hi,true", got, ok)
	}
	if got := servicectx.MustGet[int](c, kA); got != 42 {
		t.Errorf("MustGet[int]=%d want 42", got)
	}
}

// TestDuplicateKey_OverridesBeforeBuild verifies that providing the same
// key twice before Build replaces the earlier builder while preserving
// the original declaration slot for Build ordering.
func TestDuplicateKey_OverridesBeforeBuild(t *testing.T) {
	k := servicectx.NewKey[int]("only")

	c := servicectx.NewContainer().
		Provide(k, func() (any, error) { return 1, nil }).
		Provide(k, func() (any, error) { return 2, nil })

	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	if got := servicectx.MustGet[int](c, k); got != 2 {
		t.Errorf("MustGet=%d want 2 (later registration wins)", got)
	}
}

// TestProvide_AfterBuildIgnored verifies the §5.4 contract: late
// registration cannot race the resolved instance. MustGet continues to
// return the value Build produced.
func TestProvide_AfterBuildIgnored(t *testing.T) {
	k := servicectx.NewKey[int]("late")

	c := servicectx.NewContainer().
		Provide(k, func() (any, error) { return 1, nil })
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	// Late registration must be silently ignored.
	c.Provide(k, func() (any, error) { return 999, nil })
	if got := servicectx.MustGet[int](c, k); got != 1 {
		t.Errorf("MustGet=%d want 1 (post-Build Provide ignored)", got)
	}
}

// TestBuild_BuildOrderFollowsDeclaration verifies Build iterates in
// Provide order regardless of map iteration randomness.
func TestBuild_BuildOrderFollowsDeclaration(t *testing.T) {
	kA := servicectx.NewKey[int]("a")
	kB := servicectx.NewKey[int]("b")
	kC := servicectx.NewKey[int]("c")

	var buildOrder []string
	mk := func(name string, val int) func() (any, error) {
		return func() (any, error) {
			buildOrder = append(buildOrder, name)
			return val, nil
		}
	}

	c := servicectx.NewContainer().
		Provide(kA, mk("a", 1)).
		Provide(kB, mk("b", 2)).
		Provide(kC, mk("c", 3))
	if _, err := c.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(buildOrder, want) {
		t.Errorf("build order = %v, want %v", buildOrder, want)
	}
}

// TestCancel_CloseRunsInReverseOrder verifies io.Closer providers are
// closed in reverse Build order on shutdown.
func TestCancel_CloseRunsInReverseOrder(t *testing.T) {
	kA := servicectx.NewKey[io.Closer]("a")
	kB := servicectx.NewKey[io.Closer]("b")
	kC := servicectx.NewKey[io.Closer]("c")

	var closeOrder []string
	mk := func(id string) func() (any, error) {
		return func() (any, error) {
			return &fakeCloser{id: id, order: &closeOrder}, nil
		}
	}

	c := servicectx.NewContainer().
		Provide(kA, mk("a")).
		Provide(kB, mk("b")).
		Provide(kC, mk("c"))
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := cancel.Close(); err != nil {
		t.Fatalf("Cancel.Close: %v", err)
	}
	want := []string{"c", "b", "a"}
	if !reflect.DeepEqual(closeOrder, want) {
		t.Errorf("close order = %v, want %v", closeOrder, want)
	}
}

// TestCancel_CloseIsIdempotent verifies repeated Close is safe.
func TestCancel_CloseIsIdempotent(t *testing.T) {
	k := servicectx.NewKey[io.Closer]("a")
	var calls int32
	c := servicectx.NewContainer().Provide(k, func() (any, error) {
		return closeFunc(func() error { atomic.AddInt32(&calls, 1); return nil }), nil
	})
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := cancel.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cancel.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("closer called %d times, want 1", got)
	}
}

// TestBuild_FailureClosesPartial verifies a provider error rolls back
// already-built io.Closer providers before returning.
func TestBuild_FailureClosesPartial(t *testing.T) {
	kA := servicectx.NewKey[io.Closer]("a")
	kB := servicectx.NewKey[int]("b") // not an io.Closer

	var closed []string
	c := servicectx.NewContainer().
		Provide(kA, func() (any, error) {
			return &fakeCloser{id: "a", order: &closed}, nil
		}).
		Provide(kB, func() (any, error) {
			return 0, errors.New("boom")
		})

	cancel, err := c.Build()
	if err == nil {
		t.Fatal("expected Build error")
	}
	_ = errors.Is(err, errors.New("boom"))
	if cancel.Close() != nil {
		t.Fatal("Cancel.Close after failed Build must be safe (no-op)")
	}
	if !reflect.DeepEqual(closed, []string{"a"}) {
		t.Errorf("partial closer not closed: %v", closed)
	}
}

// TestBuild_TwiceErrors verifies a second Build call returns an error
// rather than silently re-running providers.
func TestBuild_TwiceErrors(t *testing.T) {
	k := servicectx.NewKey[int]("a")
	c := servicectx.NewContainer().Provide(k, func() (any, error) { return 1, nil })
	if _, err := c.Build(); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	if _, err := c.Build(); err == nil {
		t.Fatal("second Build must error")
	}
}

// TestMustGet_TypeMismatchPanics registers an int value under Key[string]
// and asserts MustGet[string] panics with a message containing both
// "string" (expected) and "int" (actual runtime type).
func TestMustGet_TypeMismatchPanics(t *testing.T) {
	k := servicectx.NewKey[string]("mismatch")
	c := servicectx.NewContainer().Provide(k, func() (any, error) { return 1, nil })
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustGet[string] over int-stored value did not panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value not string: %T", r)
		}
		if !contains(msg, "string") || !contains(msg, "int") {
			t.Errorf("panic message missing expected or actual type: %q", msg)
		}
	}()
	_ = servicectx.MustGet[string](c, k)
}

// TestMustGet_MissingKeyPanics asserts the panic message mentions both
// the missing key name and the expected type.
func TestMustGet_MissingKeyPanics(t *testing.T) {
	c := servicectx.NewContainer()
	k := servicectx.NewKey[int]("absent")
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustGet over absent key did not panic")
		}
		msg, ok := r.(string)
		if !ok || !contains(msg, "absent") || !contains(msg, "int") {
			t.Errorf("panic message missing key name or type: %q", msg)
		}
	}()
	_ = servicectx.MustGet[int](c, k)
}

// TestConcurrent_GetIsSafe verifies Get/MustGet are safe to call from
// many goroutines after Build.
func TestConcurrent_GetIsSafe(t *testing.T) {
	k := servicectx.NewKey[int]("concurrent")
	c := servicectx.NewContainer().Provide(k, func() (any, error) { return 7, nil })
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if got := servicectx.MustGet[int](c, k); got != 7 {
					t.Errorf("got %d want 7", got)
				}
			}
		}()
	}
	wg.Wait()
}

// TestNewKey_DistinctIDs verifies two NewKey[T] calls produce distinct
// keys so unrelated callers cannot accidentally collide.
func TestNewKey_DistinctIDs(t *testing.T) {
	a := servicectx.NewKey[int]("a")
	b := servicectx.NewKey[int]("a")
	if a == b {
		t.Fatal("NewKey must return distinct values even for identical args")
	}
}

// closeFunc is a tiny io.Closer adapter for tests.
type closeFunc func() error

func (f closeFunc) Close() error { return f() }

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}