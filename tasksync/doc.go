// Package tasksync is taskr's cross-device sync engine: the pure merge fold
// (Merge), the wire protocol (Request/Response, PostSync), the HTTP endpoint
// (Server), the real-time change push (Hub, Listener), and conflict detection
// (DroppedLocalEdits). It is deliberately storage- and UI-free: the ONLY thing
// it asks of the application is the one-method Store interface (fold a task
// set into storage atomically). SQL, file paths, config files and Bubble Tea
// all live on the application side of that doorway — keep it that way, so the
// engine stays independently testable and reusable.
package tasksync
