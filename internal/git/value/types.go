package value

type Scope int

const (
	ScopeUncommitted Scope = iota
	ScopeBranch
	ScopeLastCommit
	ScopeStaged
	scopeCount
)

func (s Scope) Next() Scope { return (s + 1) % scopeCount }

func (s Scope) String() string {
	switch s {
	case ScopeBranch:
		return "vs target"
	case ScopeLastCommit:
		return "last commit"
	case ScopeStaged:
		return "staged"
	default:
		return "uncommitted"
	}
}

type Repo struct {
	Root     string
	Branch   string
	Unborn   bool
	Detached bool
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
