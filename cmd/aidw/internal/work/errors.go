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
