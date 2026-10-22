package model

import "slices"

func (fd FileDiff) Clone() FileDiff {
	fd.Lines = slices.Clone(fd.Lines)
	for i := range fd.Lines {
		fd.Lines[i].Spans = slices.Clone(fd.Lines[i].Spans)
	}
	fd.Changes = slices.Clone(fd.Changes)
	fd.rows = nil
	return fd
}

func (set Set) Clone() Set {
	set.Files = slices.Clone(set.Files)
	for i := range set.Files {
		set.Files[i] = set.Files[i].Clone()
	}
	return set
}
