package work

import "errors"

// ErrNotFound is returned by Load when the requested work record's
// work.json does not exist. It wraps fs.ErrNotExist (see store.go), so
// existing errors.Is(err, fs.ErrNotExist) checks (e.g. ScanRecords)
// continue to work unchanged; ErrNotFound adds a work-domain classification
// on top, it does not replace the filesystem-level fact.
var ErrNotFound = errors.New("work record not found")

// ErrNotArchived is returned by DeleteRecord (without force) when the
// record's own Lifecycle is not yet LifecycleArchived — the per-entry
// precondition §2.2 of the Cluster I spec establishes for `work purge`.
var ErrNotArchived = errors.New("work: record is not archived; run `work archive <id>` first")

// ErrAlreadyDelivery is returned by the promote mutate closure when the
// record's Mode is already ModeDelivery — promote is a one-way transition
// (§2), so re-promoting an already-delivery record is a no-op-that-errors,
// not a silent success.
var ErrAlreadyDelivery = errors.New("work: record is already in delivery mode")

// ErrNotPromotable is returned by the promote mutate closure when the
// record's Lifecycle is neither Active nor Paused (mirroring resolve.go's
// own Active||Paused liveness rule for what counts as "live"). This guards
// against `work promote` silently mutating an archived/done record's
// on-disk footprint.
var ErrNotPromotable = errors.New("work: record is not active or paused; cannot promote an archived/done record — run `work activate <id>` first")
