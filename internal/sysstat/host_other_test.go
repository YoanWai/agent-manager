//go:build !linux

package sysstat

import "testing"

func disableHostSampling(*testing.T) {}
