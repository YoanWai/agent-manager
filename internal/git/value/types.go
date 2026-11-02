package value

type Scope int

const (
	ScopeUncommitted Scope = iota
	ScopeBranch
	ScopeCommit
	ScopeStaged
	scopeCount
)

func (s Scope) Next() Scope { return (s + 1) % scopeCount }

func (s Scope) String() string {
	switch s {
	case ScopeBranch:
		return "vs target"
	case ScopeCommit:
		return "commit"
	case ScopeStaged:
		return "staged"
	default:
		return "uncommitted"
	}
}

type Repo struct {
	Root     string
	Branch   string
	Head     string
	Unborn   bool
	Detached bool
}

// Commit is one commit the commit scope can review, with the paths its diff
// against its first parent changes.
type Commit struct {
	SHA     string
	Short   string
	Subject string
	Files   []string
}

type Status byte

const (
	Added     Status = 'A'
	Modified  Status = 'M'
	Deleted   Status = 'D'
	Renamed   Status = 'R'
	Copied    Status = 'C'
	Untracked Status = '?'
	Unmerged  Status = 'U'
)

type ChangedFile struct {
	Path    string
	OldPath string
	Status  Status
}

type FileStat struct {
	Adds, Dels int
	Binary     bool
}

type Worktree struct {
	Root   string
	Branch string
}
