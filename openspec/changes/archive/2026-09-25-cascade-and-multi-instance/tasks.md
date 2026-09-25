# Tasks

## 1. Domain Model & Port

- [x] 1.1 Add `CascadeRoute` model to `internal/domain/model/cascade.go` with `FromDeviceID`, `RoutePath []string`, `PreferredPath []string`; add `ParseRoutePath`/`FormatRoutePath` helpers with unit tests covering whitespace and trailing-comma edge cases
- [x] 1.2 Add `CascadeHandler` interface to `internal/domain/port/cascade.go` exposing `Forward(fromNodeID, msg, dstDeviceID)`; verify the port compiles against a no-op stub
- [x] 1.3 Extend `NodeProfile` with optional cascade parent/children; verify validation round-trip in existing profile test

## 2. Cascade Handler Implementation

- [x] 2.1 Implement `adapter/cascade/handler.go`: append current node ID to X-RoutePath, resolve next hop from X-PreferredPath (falling back to CascadeChildren/topology), and return next-hop + headers; verify with unit tests on header strings
- [x] 2.2 Implement loop detection: if current node ID already appears in X-RoutePath, stop forwarding with a descriptive error; verify with unit test feeding a self-referential path
- [x] 2.3 Implement upstream propagation: when the destination is not a direct child, route to CascadeParent; verify with handler test

## 3. Nodereg Wiring (Topology Hot-Swap + Acyclic Validation)

- [x] 3.1 Inject `port.CascadeHandler` into `adapter/nodereg.Registry`; refresh the handler topology on `Register`/`Unregister`
- [x] 3.2 Add cascade acyclic validation on `Register` (reject parent-child cycles); verify dedicated register test returns error on cycle setup
- [x] 3.3 Add topology hot-swap unit test: swap in a topology and assert child lookup works after swap

## 4. App Layer Outbound Injection

- [x] 4.1 Wire `CascadeHandler.Forward` into `internal/app/acceptor.go` outbound sends (responses and NOTIFY)
- [x] 4.2 Inject returned `X-RoutePath`/`X-PreferredPath` headers into outbound `model.Message`; destination always stays the original peer (headers only mark the path)

## 5. Composition Root

- [x] 5.1 In `cmd/gb28181-simulator/main.go`, construct the cascade handler with the initial `TopologyMap` and inject into the node registry
- [x] 5.2 Run `go build ./...` and confirm no import cycles

## 6. End-to-End & Multi-Instance Tests

- [x] 6.1 Add `TestThreeNodeMultiHop` in `adapter/cascade/handler_test.go`: 3-node topology (large → small → device), query from large, assert RoutePath at each hop and loop detection
- [x] 6.2 Add preferred-path routing test: PreferredPath entry that matches a direct neighbour is consumed; verify outgoing headers
- [x] 6.3 Run `go test ./... -count=1` and confirm all packages pass
