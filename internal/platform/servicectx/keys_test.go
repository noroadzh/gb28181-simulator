package servicectx

import (
	"context"
	"errors"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// fakeStore implements port.Storage so the container can be exercised
// without pulling in the real adapter.
type fakeStore struct{ closed bool }

func (f *fakeStore) CRUD(context.Context, interface{}) error { return nil }
func (f *fakeStore) List(context.Context, interface{}) (interface{}, error) {
	return nil, nil
}
func (f *fakeStore) Close() error { f.closed = true; return nil }

// TestStorageKey_ResolvesToPortStorage asserts the well-known storage key is
// typed by the domain port, so the composition root retrieves a
// port.Storage and this package stays free of adapter types (design D10).
func TestStorageKey_ResolvesToPortStorage(t *testing.T) {
	store := &fakeStore{}
	c := NewContainer().Provide(StorageKey, func() (any, error) {
		return store, nil
	})
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cancel.Close()

	got := MustGet[port.Storage](c, StorageKey)
	if got == nil {
		t.Fatal("MustGet[port.Storage] returned nil")
	}
	// The concrete value must survive the type-erased round trip.
	if _, ok := got.(*fakeStore); !ok {
		t.Errorf("got %T, want *fakeStore", got)
	}
	// And GetTyped must agree.
	if _, ok := GetTyped[port.Storage](c, StorageKey); !ok {
		t.Error("GetTyped[port.Storage] returned false")
	}
}

// TestStorageKey_CloseRunsOnCancel asserts the store is closed through the
// port's io.Closer when the container is cancelled.
func TestStorageKey_CloseRunsOnCancel(t *testing.T) {
	store := &fakeStore{}
	c := NewContainer().Provide(StorageKey, func() (any, error) {
		return store, nil
	})
	cancel, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := cancel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !store.closed {
		t.Error("store was not closed by Cancel.Close")
	}
}

// TestStorageKey_ProviderErrorSurfaces asserts a failing provider is
// reported by Build rather than producing half-built state.
func TestStorageKey_ProviderErrorSurfaces(t *testing.T) {
	c := NewContainer().Provide(StorageKey, func() (any, error) {
		return nil, errors.New("boom")
	})
	if _, err := c.Build(); err == nil {
		t.Fatal("Build succeeded despite a failing provider")
	}
}
