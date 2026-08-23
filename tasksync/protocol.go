package tasksync

import "fmt"

// Wire protocol versioning.
//
// Sync is the one place where two taskr binaries of different vintages talk
// to each other — that is the entire reason it exists — so the wire needs a
// version on it. Without one, a client sending a field the server does not
// know, or expecting a field the server does not send, fails by silent
// misinterpretation: the merge runs on whatever decoded, and the user finds
// out when data is wrong rather than when the request is made.
//
// The rule is: every request carries the version it was written against, and
// a server that cannot speak it says so in a message naming which side is
// behind. That turns an invisible data bug into a legible upgrade prompt.
const (
	// ProtocolVersion is the wire version this build speaks. Bump it in the
	// same commit that changes the meaning of a field in Request or Response
	// — adding a field that older peers can safely ignore does not need one.
	ProtocolVersion = 1

	// MinProtocolVersion is the oldest client wire this build still accepts.
	// Raise it only to drop support deliberately; every raise strands the
	// clients that still speak the older version.
	MinProtocolVersion = 1

	// legacyProtocolVersion is what an absent version field means. Clients
	// released before versioning existed send no "v" at all, and they speak
	// exactly what v1 describes — so absent is v1, not an error. Removing
	// this assumption would break every deployed client at once.
	legacyProtocolVersion = 1
)

// ProtocolHeader carries the server's supported range on unauthenticated
// health responses, so an operator can check what a server speaks without
// holding the sync token.
const ProtocolHeader = "Taskr-Sync-Protocol"

// VersionHeader carries the taskr build the server is running, on *every*
// response including errors.
//
// The wire version above only moves when a field changes meaning, so two
// builds years apart still negotiate cleanly — which is the point, and also
// why it cannot answer "why did this stop working". The failure it misses is
// two taskr builds of different vintages against one store: the newer one
// migrates the schema, the older server process keeps running and answers
// every sync with a 500 whose body ("no such table: task_learnings") means
// nothing to the person reading it. A header rather than a body field because
// that failure produces no decodable body at all — an http.Error — and that is
// exactly the moment the client needs to be able to name which side is old.
const VersionHeader = "Taskr-Version"

// negotiate validates a peer's declared version against what this build
// supports, returning a user-facing error when they cannot talk. A zero
// version means the peer predates versioning and is treated as v1.
func negotiate(peer int) error {
	if peer == 0 {
		peer = legacyProtocolVersion
	}
	if peer > ProtocolVersion {
		return fmt.Errorf("sync protocol mismatch: the other side speaks v%d but this taskr only understands up to v%d — upgrade this end", peer, ProtocolVersion)
	}
	if peer < MinProtocolVersion {
		return fmt.Errorf("sync protocol mismatch: the other side speaks v%d but this taskr no longer supports anything below v%d — upgrade that end", peer, MinProtocolVersion)
	}
	return nil
}
