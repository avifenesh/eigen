package llm

// Batch inference: an OPTIONAL provider capability for off-hot-path,
// latency-insensitive work (currently dream Stage1). A batch is fundamentally
// async — submit many independent requests now, the provider processes them
// within a window (minutes–hours), poll, then collect — which doesn't fit the
// synchronous Provider.Complete contract. So it's a SEPARATE optional interface
// a provider MAY also satisfy; callers probe with AsBatch and fall back to a
// plain Complete loop when it's absent. Only providers with a real, reachable
// batch API implement it (Anthropic Messages Batches today; ~50% input
// discount). NOT a change to Provider.

import "context"

// BatchItem is one request in a batch, tagged with a caller correlation Key
// (e.g. a session/thread id) so results map back unambiguously — the provider
// returns results keyed by this id, in any order.
type BatchItem struct {
	Key string
	Req Request
}

// BatchState is the lifecycle phase of a submitted batch.
type BatchState string

const (
	BatchPending    BatchState = "pending"    // submitted, not yet started
	BatchProcessing BatchState = "processing" // in flight
	BatchDone       BatchState = "done"       // results ready to collect
	BatchFailed     BatchState = "failed"     // provider-side failure
	BatchExpired    BatchState = "expired"    // window elapsed; results gone
)

// BatchStatus is a poll snapshot.
type BatchStatus struct {
	State BatchState
	Done  int // completed items (best-effort; 0 when the provider doesn't report)
	Total int
}

// Terminal reports whether no further polling will change the state.
func (s BatchStatus) Terminal() bool {
	return s.State == BatchDone || s.State == BatchFailed || s.State == BatchExpired
}

// BatchProvider is the optional async-batch capability. A provider that has a
// batch API implements it ALONGSIDE Provider. The lifecycle is:
//
//	id, _   := bp.SubmitBatch(ctx, items)   // persist id immediately (crash-safe)
//	st, _   := bp.PollBatch(ctx, id)        // on each daemon wake
//	if st.State == BatchDone {
//	    res, _ := bp.CollectBatch(ctx, id)  // map[Key]*Response
//	}
//
// CollectBatch returns one *Response per item Key that succeeded; a Key absent
// from the map failed individually (the caller reprocesses it synchronously).
type BatchProvider interface {
	SubmitBatch(ctx context.Context, items []BatchItem) (id string, err error)
	PollBatch(ctx context.Context, id string) (BatchStatus, error)
	CollectBatch(ctx context.Context, id string) (map[string]*Response, error)
}

// AsBatch reports whether p supports batch inference, returning the capability
// view when it does. Callers (the dream pipeline) use this to choose the batch
// path vs a synchronous Complete loop — so an unsupported provider, or a
// future one, degrades gracefully with no special-casing.
func AsBatch(p Provider) (BatchProvider, bool) {
	bp, ok := p.(BatchProvider)
	return bp, ok
}
