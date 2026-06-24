// Frontend mirror of the Go event-name builders (pump.go / bridge.go) plus a
// thin typed wrapper over the Wails runtime Events API. Every On() returns its
// unsubscribe fn — callers MUST register inside an $effect and return the
// remover so no listener ever leaks.
import { Events } from "@wailsio/runtime";

export const ev = {
  sessionEvent: (id: string) => `eigen:session:${id}:event`,
  sessionClosed: (id: string) => `eigen:session:${id}:closed`,
  daemonStats: "eigen:daemon:stats",
  daemonHealth: "eigen:daemon:health",
  feed: "eigen:feed",
  voice: "eigen:voice",
};

// on subscribes to a Wails event and hands the typed payload to the callback,
// returning the unsubscribe function verbatim.
export function on<T>(name: string, cb: (data: T) => void): () => void {
  return Events.On(name, (e: { data: T }) => cb(e.data));
}
