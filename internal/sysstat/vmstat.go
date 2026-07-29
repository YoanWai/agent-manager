package sysstat

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	vmStatPageSize = regexp.MustCompile(`page size of (\d+) bytes`)
	vmStatLine     = regexp.MustCompile(`^([^:]+):\s+(\d+)\.?$`)
)

// parseVMStatMemoryUsed implements Activity Monitor "Memory Used": all
// resident RAM except free, speculative (treated as free), and reclaimable
// cache (file-backed + purgeable). That is total minus those page classes,
// which also counts kernel/other pages that app+wired+compressed omits
// (~0.5–1 GiB on a loaded Mac) and matches the Memory tab more closely.
func parseVMStatMemoryUsed(out string, total, defaultPageSize uint64) (uint64, bool) {
	pageSize := defaultPageSize
	if pageSize == 0 {
		pageSize = 4096
	}
	pages := map[string]uint64{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if m := vmStatPageSize.FindStringSubmatch(line); len(m) == 2 {
			if n, err := strconv.ParseUint(m[1], 10, 64); err == nil && n > 0 {
				pageSize = n
			}
			continue
		}
		m := vmStatLine.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		n, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil {
			continue
		}
		pages[strings.TrimSpace(m[1])] = n
	}

	free, okFree := pages["Pages free"]
	if !okFree {
		return 0, false
	}
	speculative := pages["Pages speculative"]
	purgeable := pages["Pages purgeable"]
	fileBacked, okFile := pages["File-backed pages"]
	if okFile && total > 0 {
		reclaimable := free + speculative + fileBacked + purgeable
		totalPages := total / pageSize
		if reclaimable >= totalPages {
			return 0, true
		}
		return (totalPages - reclaimable) * pageSize, true
	}

	// Older macOS without file-backed: App + Wired + Compressed.
	wired, okW := pages["Pages wired down"]
	anon, okA := pages["Anonymous pages"]
	if !okW || !okA {
		return 0, false
	}
	compressor, okC := pages["Pages occupied by compressor"]
	if !okC {
		compressor, okC = pages["Pages used by compressor"]
	}
	if !okC {
		return 0, false
	}
	app := anon
	if anon >= purgeable {
		app = anon - purgeable
	} else {
		app = 0
	}
	return (app + wired + compressor) * pageSize, true
}
