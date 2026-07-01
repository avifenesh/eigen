package gui

// Emitter is the seam between the Bridge and whatever host delivers its events
// to a frontend. The Wails desktop build satisfies it with an adapter over the
// Wails app's event bus (wails.go); the headless guiserver satisfies it with a
// socket fan-out to subscribed clients. The seam exists so this package never
// names a Wails type on its emission path and therefore compiles tagless — no
// webkitgtk — which is what lets guiserver join `make gate`.
//
// Contract: Emit must be non-blocking (or near-instant). Pump handlers run on
// a daemon connection's single event-loop goroutine, so an Emit that blocks
// stalls that session's entire stream (see pump.go).
type Emitter interface {
	Emit(name string, data any)
}
