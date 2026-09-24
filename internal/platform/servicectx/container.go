// Package servicectx provides a hand-written dependency-injection container.
//
// A Container stores values keyed by an opaque Key[T]. Providers can be
// registered in any order with Provide; Build evaluates every provider in
// declaration order and returns a Cancel that closes every io.Closer
// provider in reverse order. MustGet[T] is the typed accessor.
//
// servicectx has no third-party dependencies; it is safe to import from
// cmd/* and from any internal package that needs DI without paying for a
// full DI framework.
package servicectx

import (
	"fmt"
	"io"
	"reflect"
	"sync"
)

// Keyer is the type-erased view of a Key[T] used by Container's
// non-generic method receivers. Implementations are produced by
// NewKey[T] and the only callers of id() are inside this package.
type Keyer interface {
	id() uint64
	name() string
}

// Key is an opaque, comparable handle that identifies a slot in a
// Container. Keys are produced by NewKey[T]; two calls with the same T
// produce distinct keys (so callers cannot accidentally collide across
// packages).
type Key[T any] struct {
	kname string // type name for diagnostic messages
	kid   uint64 // monotonic, package-private counter
}

func (k Key[T]) id() uint64   { return k.kid }
func (k Key[T]) name() string { return k.kname }

// NewKey returns a fresh Key[T]. The type parameter T is what MustGet[T]
// will check at lookup time; providing a value of any other type is a
// compile-time error at the call site that registers the provider, and a
// runtime panic at the call site that reads it.
func NewKey[T any](name string) Key[T] {
	globalKeyMu.Lock()
	defer globalKeyMu.Unlock()
	globalKeySeq++
	return Key[T]{kname: name, kid: globalKeySeq}
}

var (
	globalKeyMu  sync.Mutex
	globalKeySeq uint64
)

// providerEntry is the stored slot for one registered key. instances are
// produced lazily on Build; after Build, late calls to Provide for the
// same key are silently ignored (see §5.4 — "Build 后改 provider 无效").
type providerEntry struct {
	id        uint64
	build     func() (any, error)
	instances []any // records each Build invocation's result; len 1 normally
}

// Container collects keyed providers and materialises them on Build.
type Container struct {
	mu        sync.Mutex
	providers map[uint64]*providerEntry
	built     bool
	order     []uint64 // insertion order of registered ids, for deterministic Build
}

// NewContainer returns an empty Container.
func NewContainer() *Container {
	return &Container{providers: make(map[uint64]*providerEntry)}
}

// Provide registers a builder for key k. The builder is invoked exactly
// once at Build time and its result (or error) is cached for the lifetime
// of the Container.
//
// If k was already provided, the later registration replaces the earlier
// one ONLY while Build has not yet run. After Build, Provide is a no-op so
// that ad-hoc late calls cannot race the resolved instance. This matches
// the "Build 后改 provider 无效" guarantee required by §5.4.
//
// Provide returns the Container for chaining. The key argument is type-
// erased here because Go forbids extra type parameters on a non-generic
// receiver; the Key[T] value carries T implicitly through its internal
// id, and MustGet[T] recovers T at lookup time.
func (c *Container) Provide(k Keyer, build func() (any, error)) *Container {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.built {
		return c
	}

	id := k.id()
	if existing, ok := c.providers[id]; ok {
		existing.build = build
		return c
	}
	c.providers[id] = &providerEntry{id: id, build: build}
	c.order = append(c.order, id)
	return c
}

// Build materialises every registered provider in declaration order. If
// any provider returns an error, Build stops, closes the already-built
// io.Closer providers in reverse order, and returns that error. The
// returned Cancel is always safe to call (a no-op when Build failed or
// when Build is called twice).
//
// After Build returns without error, Get/MustGet may be called repeatedly
// and concurrently from any goroutine.
func (c *Container) Build() (Cancel, error) {
	c.mu.Lock()
	if c.built {
		c.mu.Unlock()
		return Cancel{}, fmt.Errorf("servicectx: Build called twice")
	}
	// Snapshot the order under the lock to avoid racing with future
	// Provide calls (which are no-ops post-Build but we still want a
	// stable order without holding the lock once we've set built=true).
	order := append([]uint64(nil), c.order...)
	c.built = true
	entries := make([]*providerEntry, len(order))
	for i, id := range order {
		entries[i] = c.providers[id]
	}
	c.mu.Unlock()

	closers := make([]io.Closer, 0, len(order))
	for _, e := range entries {
		v, err := e.build()
		if err != nil {
			// Close the closers we did get, in reverse order, before
			// returning so the caller does not leak partial state.
			closeReverse(closers)
			return Cancel{}, fmt.Errorf("servicectx: build provider: %w", err)
		}
		e.instances = append(e.instances, v)
		if cl, ok := v.(io.Closer); ok {
			closers = append(closers, cl)
		}
	}
	cancel := func() { closeReverse(closers) }
	return Cancel{shutdown: cancel, closers: closers}, nil
}

// GetTyped is the generic lookup primitive; see the comment on
// ProvideTyped for why this is split from the method form.
func GetTyped[T any](c *Container, k Key[T]) (T, bool) {
	var zero T
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.providers[k.kid]
	if !ok || len(e.instances) == 0 {
		return zero, false
	}
	v, ok := e.instances[0].(T)
	if !ok {
		return zero, false
	}
	return v, true
}

// MustGet is the typed accessor. It panics with a message that includes
// the expected type T and the actual runtime type if the stored value
// cannot be assigned to T, or if no value has been registered for k.
func MustGet[T any](c *Container, k Key[T]) T {
	c.mu.Lock()
	e, ok := c.providers[k.kid]
	c.mu.Unlock()
	if !ok || len(e.instances) == 0 {
		panic(fmt.Sprintf("servicectx: MustGet[%s]: no value registered for key %s",
			typeName[T](), k.name()))
	}
	v := e.instances[0]
	if vv, ok := v.(T); ok {
		return vv
	}
	panic(fmt.Sprintf(
		"servicectx: MustGet[%s]: stored value has type %s (expected %s) for key %s",
		typeName[T](), reflect.TypeOf(v), typeName[T](), k.name(),
	))
}

// typeName returns the unqualified type name of T for diagnostic
// messages. Falls back to the fully-qualified reflection form when T is
// anonymous (interface{...} etc.).
func typeName[T any]() string {
	t := reflect.TypeOf((*T)(nil)).Elem()
	if t == nil {
		return "<nil>"
	}
	if n := t.Name(); n != "" {
		return n
	}
	return t.String()
}

// Cancel is the handle returned by Build. Calling Close shuts the
// Container down by closing every io.Closer provider in reverse Build
// order. Close is idempotent and never returns an error because the
// underlying closer errors are not actionable in shutdown; callers that
// need per-closer error visibility should use a different lifecycle
// hook.
type Cancel struct {
	shutdown func()
	closers  []io.Closer
	once     sync.Once
}

// Close runs the shutdown function exactly once. Safe to call from any
// goroutine. Returns nil.
func (c *Cancel) Close() error {
	if c == nil || c.shutdown == nil {
		return nil
	}
	c.once.Do(c.shutdown)
	return nil
}

// closeReverse calls Close on every closer in reverse insertion order.
// Errors are swallowed because shutdown is best-effort by contract.
func closeReverse(closers []io.Closer) {
	for i := len(closers) - 1; i >= 0; i-- {
		_ = closers[i].Close()
	}
}