package value

// Trouble is the kind of failure that kept a call from a host's manager, so
// a row can say it in a word or two.
type Trouble uint8

const (
	// TroubleFailed is any other failure, such as the command refusing.
	TroubleFailed Trouble = iota
	// TroubleUnreachable is ssh's own failure, or a host that did not
	// answer in time.
	TroubleUnreachable
	// TroubleMissing is no agent-manager where the call looked for it.
	TroubleMissing
	// TroubleOutdated is an agent-manager there too old for this one.
	TroubleOutdated
	// TroubleNewer is an agent-manager there newer than this one.
	TroubleNewer
)
