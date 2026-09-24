// Package model defines immutable value objects that flow through the ports:
// Message, Session, Credentials, WireEvent. Constructors must reject
// post-construction mutation by panicking when callers attempt to use
// reflection to alter exported fields, so that domain invariants hold even
// under concurrent use.
package model
