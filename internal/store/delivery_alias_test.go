package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryGuardUsesOneIdentityThroughDatabaseSymlink(t *testing.T) {
	dir := t.TempDir()
	primary, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	alias := filepath.Join(dir, "alias.db")
	if err := os.Symlink(primary.path, alias); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	acquired, err := primary.WithDeliveryGuard(context.Background(), func(*DeliveryGuard) error {
		peerAcquired, peerErr := peer.WithDeliveryGuard(context.Background(), func(*DeliveryGuard) error { return nil })
		if peerErr != nil {
			t.Fatal(peerErr)
		}
		if peerAcquired {
			t.Fatal("database alias admitted a second owner")
		}
		return nil
	})
	if err != nil || !acquired {
		t.Fatalf("guard acquired=%v err=%v", acquired, err)
	}
}
