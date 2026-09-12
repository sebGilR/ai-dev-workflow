package work

import "github.com/oklog/ulid/v2"

// NewID returns a new ULID string (26 chars, sortable by creation time)
// using github.com/oklog/ulid/v2 with monotonic entropy so IDs generated
// within the same millisecond in one process still sort correctly.
func NewID() string {
	return ulid.Make().String()
}
