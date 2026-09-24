// Package clock exposes a Clock interface so that time-sensitive code can
// be unit-tested with a Fake. The Real() implementation is wired in main;
// the Fake() implementation is the test fixture used across the suite.
// Concrete implementation lands in Change 3 §3.3.
package clock
