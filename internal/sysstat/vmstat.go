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

// parseVMStatMemoryUsed implements Activity Monitor "Memory Used" from a
// vm_stat snapshot: (anonymous - purgeable) + wired + compressor pages.
func parseVMStatMemoryUsed(out string, defaultPageSize uint64) (uint64, bool) {
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
	purgeable := pages["Pages purgeable"]
	app := anon
	if anon >= purgeable {
		app = anon - purgeable
	} else {
		app = 0
	}
	return (app + wired + compressor) * pageSize, true
}
