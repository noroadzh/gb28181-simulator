// Package app hosts the use-case orchestrators (Service, BackgroundClock,
// Stats, ...). Each service struct accepts only domain ports in its
// constructor and exposes intent-revealing methods. Concrete services are
// added in Change 4 `node-abstraction`; this change only lays the directory.
package app