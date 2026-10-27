package connections

import (
	"testing"
	"time"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
)

var (
	box = remote.Connection{Name: "box", Destination: "me@box"}
	gpu = remote.Connection{Name: "gpu", Destination: "me@gpu"}
)

func TestRepointingAConnectionDropsItsLastRefresh(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box, gpu})
	c.Refreshed("box", c.Gen(), Host{OK: true})
	c.Refreshed("gpu", c.Gen(), Host{OK: true})
	c.SetList([]remote.Connection{{Name: "box", Destination: "me@other"}, gpu})
	if _, seen := c.Host("box"); seen {
		t.Fatal("box kept the refresh of its old destination")
	}
	if _, seen := c.Host("gpu"); !seen {
		t.Fatal("gpu lost its refresh to an edit of box")
	}
}

func TestARefreshForAnEarlierListIsDropped(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	gen := c.Gen()
	c.SetList([]remote.Connection{box})
	if c.Refreshed("box", gen, Host{OK: true}) {
		t.Fatal("a refresh started before the list changed landed")
	}
}

func TestOneRefreshAtATimeAndAFailedHostRests(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	now := time.Now()
	if !c.StartRefresh("box", now) || c.StartRefresh("box", now) {
		t.Fatal("want one refresh in flight")
	}
	if c.StartRefresh("gone", now) {
		t.Fatal("a connection not listed was refreshed")
	}
	c.Refreshed("box", c.Gen(), Host{Err: "box: unreachable over SSH", Failures: 1, Attempted: now})
	if c.StartRefresh("box", now) {
		t.Fatal("a host that just failed was refreshed again at once")
	}
	if !c.StartRefresh("box", now.Add(remote.RetryWait(1))) {
		t.Fatal("a host past its wait was not refreshed")
	}
	if refusal, offline := c.Offline("box"); !offline || refusal != "box: unreachable over SSH" {
		t.Fatalf("offline = %q, %v", refusal, offline)
	}
}

// A host never reached yet is still tried.
func TestAnUnseenHostIsNotOffline(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	if _, offline := c.Offline("box"); offline {
		t.Fatal("a host never refreshed refused calls")
	}
}

func TestAnInstallLeavesAConnectionWithItsOwnPathAlone(t *testing.T) {
	var c Connections
	lab := remote.Connection{Name: "lab", Destination: "me@lab", Binary: "~/bin/agent-manager"}
	c.SetList([]remote.Connection{box, lab})
	c.Refreshed("box", c.Gen(), Host{Installable: true})
	c.Refreshed("lab", c.Gen(), Host{Installable: true})
	if !c.Installable("box") || c.Installable("lab") {
		t.Fatalf("installable: box %v, lab %v; want only box", c.Installable("box"), c.Installable("lab"))
	}
}

func TestAnInstallsRefreshLandsOnlyOnItsList(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	c.Refreshed("box", c.Gen(), Host{Installable: true})
	gen := c.Gen()
	c.StartInstall("box")
	c.FinishInstall("box", gen, "checksum mismatch", Host{})
	if c.Installing("box") || c.InstallFailed("box") != "checksum mismatch" || !c.Installable("box") {
		t.Fatalf("after a failed install: installing %v failed %q", c.Installing("box"), c.InstallFailed("box"))
	}
	c.StartInstall("box")
	c.SetList([]remote.Connection{box})
	c.FinishInstall("box", gen, "", Host{OK: true})
	if host, _ := c.Host("box"); host.OK || c.InstallFailed("box") != "" {
		t.Fatalf("an install for an earlier list landed: %+v", host)
	}
}
