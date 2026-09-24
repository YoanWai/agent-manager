//go:build darwin

package sysstat

import "testing"

func TestParseDarwinBattery(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><array><dict>
<key>CurrentCapacity</key><integer>43</integer>
<key>MaxCapacity</key><integer>100</integer>
<key>IsCharging</key><true/>
</dict></array></plist>`)
	got, err := parseDarwinBattery(data)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[0].Current != 43 || got[0].Full != 100 || got[0].State.Raw.String() != "Charging" {
		t.Fatalf("got %+v", got[0])
	}
}
