//go:build linux

package sysstat

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shirou/gopsutil/v4/sensors"
)

// Runs the real Linux reader over a sysfs tree we lay out ourselves.
func TestReadTempsFromSysfs(t *testing.T) {
	cases := []struct {
		name  string
		hwmon []hwmonChip
		zones []thermalZone
		want  tempReading
	}{
		{
			name: "intel laptop with a coretemp package and an nvme",
			hwmon: []hwmonChip{
				{driver: "coretemp", sensors: []hwmonSensor{
					{label: "Package id 0", milliC: 58000},
					{label: "Core 0", milliC: 54000},
				}},
				{driver: "nvme", sensors: []hwmonSensor{{label: "Composite", milliC: 47000}}},
			},
			want: tempReading{cpu: 58, cpuOK: true},
		},
		{
			name: "amd desktop with a discrete radeon",
			hwmon: []hwmonChip{
				{driver: "k10temp", sensors: []hwmonSensor{
					{label: "Tctl", milliC: 64000},
					{label: "Tccd1", milliC: 59000},
				}},
				{driver: "amdgpu", sensors: []hwmonSensor{{label: "edge", milliC: 49000}}},
			},
			want: tempReading{cpu: 64, cpuOK: true, gpu: 49, gpuOK: true},
		},
		{
			name:  "a board with no hwmon falls back to its thermal zones",
			zones: []thermalZone{{kind: "cpu-thermal", milliC: 52000}},
			want:  tempReading{cpu: 52, cpuOK: true},
		},
		{
			name:  "an soc thermal zone reads as the whole chip",
			zones: []thermalZone{{kind: "soc-thermal", milliC: 48000}},
			want:  tempReading{soc: 48, socOK: true},
		},
		{
			name: "a kernel with no sensor files reports nothing",
			want: tempReading{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOST_SYS", writeSysfs(t, tc.hwmon, tc.zones))
			read, err := sensors.SensorsTemperatures()
			if err != nil && len(read) == 0 && (len(tc.hwmon) > 0 || len(tc.zones) > 0) {
				t.Fatalf("reading sensors: %v", err)
			}
			if got := classifyTemps(read); got != tc.want {
				t.Fatalf("classifyTemps() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

type hwmonSensor struct {
	label  string
	milliC int
}

type hwmonChip struct {
	driver  string
	sensors []hwmonSensor
}

type thermalZone struct {
	kind   string
	milliC int
}

func writeSysfs(t *testing.T, chips []hwmonChip, zones []thermalZone) string {
	t.Helper()
	root := t.TempDir()
	for i, chip := range chips {
		dir := filepath.Join(root, "class", "hwmon", "hwmon"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating hwmon dir: %v", err)
		}
		writeSysfsFile(t, filepath.Join(dir, "name"), chip.driver)
		for j, sensor := range chip.sensors {
			prefix := filepath.Join(dir, "temp"+strconv.Itoa(j+1))
			writeSysfsFile(t, prefix+"_label", sensor.label)
			writeSysfsFile(t, prefix+"_input", strconv.Itoa(sensor.milliC))
		}
	}
	for i, zone := range zones {
		dir := filepath.Join(root, "class", "thermal", "thermal_zone"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating thermal zone dir: %v", err)
		}
		writeSysfsFile(t, filepath.Join(dir, "type"), zone.kind)
		writeSysfsFile(t, filepath.Join(dir, "temp"), strconv.Itoa(zone.milliC))
	}
	return root
}

func writeSysfsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
