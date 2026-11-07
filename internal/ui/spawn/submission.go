package spawn

// Submission identifies one accepted prompt within a composer.
type Submission struct {
	ID    uint64
	Draft string
	Saved []Saved
}
