package work

import "errors"

// ErrNotFound is returned by Load when the requested work record's
// work.json does not exist. It wraps fs.ErrNotExist (see store.go), so
// existing errors.Is(err, fs.ErrNotExist) checks (e.g. ScanRecords)
// continue to work unchanged; ErrNotFound adds a work-domain classification
// on top, it does not replace the filesystem-level fact.
var ErrNotFound = errors.New("work record not found")
