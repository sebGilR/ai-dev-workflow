// Package work implements the work-model store: a repo-agnostic,
// worktree-survivable record of "what work exists" (Record), session
// bindings, and the shared resolver used by the aidw work command tree and
// the save-wip-snapshot hook. work imports state for storage-location and
// locking primitives; it never imports wip.
package work

import "errors"

// CurrentSchemaVersion is the schema version this build of work writes and
// the only version Load accepts.
const CurrentSchemaVersion = 1

// ErrUnsupportedSchemaVersion is returned by Load when a record's
// schema_version is missing or does not match CurrentSchemaVersion. Load
// never silently proceeds on a mismatch and never panics.
var ErrUnsupportedSchemaVersion = errors.New("unsupported work record schema version")

// ErrNoActiveWork is returned by Resolve when no candidate work record
// matches the given context.
var ErrNoActiveWork = errors.New("no active work for this context")

// ErrAmbiguousWork is returned by Resolve when more than one candidate work
// record matches the given context and none can be disambiguated
// automatically.
var ErrAmbiguousWork = errors.New("multiple work records match; specify --work")

// ErrIncompleteScan is returned (always wrapped TOGETHER with
// ErrAmbiguousWork) by Resolve when the record scan backing step 3 could not
// read one or more record directories. An incomplete scan cannot support a
// confident answer: the record it failed to read may well have been a second
// match, so what looks like "exactly one candidate" may actually be
// ambiguity in disguise.
//
// It is deliberately wrapped alongside ErrAmbiguousWork so every existing
// caller's `errors.Is(err, ErrAmbiguousWork)` branch does the right thing —
// list candidates, exit non-zero, and crucially NEVER auto-bind the session
// — while callers that want to explain the real cause can test for
// ErrIncompleteScan.
var ErrIncompleteScan = errors.New("work record scan was incomplete; some records could not be read")

// Mode distinguishes a delivery-style work item (branch/PR-oriented) from a
// freeform one (not built until Cluster J).
type Mode string

const (
	ModeDelivery Mode = "delivery"
	ModeFreeform Mode = "freeform" // valid enum value; not produced by G3
)

// Lifecycle is the coarse state of a work record. Transitions between these
// (pause/done/archive/purge) are Cluster I — not implemented here.
type Lifecycle string

const (
	LifecycleActive   Lifecycle = "active"
	LifecyclePaused   Lifecycle = "paused"
	LifecycleDone     Lifecycle = "done"
	LifecycleArchived Lifecycle = "archived"
)

// Context is the freeform working notes carried on a Record.
type Context struct {
	Goal          string   `json:"goal"`
	Constraints   []string `json:"constraints"`
	Decisions     []string `json:"decisions"`
	OpenQuestions []string `json:"open_questions"`
	NextAction    string   `json:"next_action"`
}

// Attachment binds a Record to one repo/worktree/branch/commit context.
type Attachment struct {
	RepoID       string `json:"repo_id"`
	WorktreePath string `json:"worktree_path"` // EvalSymlinks-normalized, see store.go
	Branch       string `json:"branch"`
	Head         string `json:"head"`
}

// Provenance tracks schema/authoring metadata for a Record.
type Provenance struct {
	SchemaVersion    int               `json:"schema_version"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	SourceHashes     map[string]string `json:"source_hashes"`
	AuthoringSession string            `json:"authoring_session"`
}

// Record is the durable, worktree-survivable unit of the work-model store.
type Record struct {
	SchemaVersion int       `json:"schema_version"` // authoritative for Load's version check
	WorkID        string    `json:"work_id"`
	Title         string    `json:"title"`
	Mode          Mode      `json:"mode"`
	Lifecycle     Lifecycle `json:"lifecycle"`
	// Stage is delivery-mode only, opaque, unvalidated. If a future cluster
	// adds stage validation, derive allowed values from wip's own stages
	// map (cmd/aidw/internal/wip/wip.go's `stages`) at call time — do not
	// duplicate that list here; it has already drifted from the design
	// doc's prose once ("pr-prep" vs. "pr-prepped").
	Stage        string       `json:"stage,omitempty"`
	Context      Context      `json:"context"`
	Attachments  []Attachment `json:"attachments"`
	InitiativeID *string      `json:"initiative_id"` // reserved, unused
	Provenance   Provenance   `json:"provenance"`
}
