// Package servicectx provides Container, a hand-written dependency-injection
// container used by cmd/gb28181-simulator and cmd/sipprobe to wire
// providers, build them in declaration order, and close them in reverse
// order on shutdown. MustGet[T] is the typed accessor. Concrete
// implementation lands in Change 3 §5.
package servicectx