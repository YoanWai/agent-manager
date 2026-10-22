package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func TestDismissNoticeMergesSortedUniqueIDs(t *testing.T) {
	st := newTestStore(t)
	if err := st.DismissNotice(""); err == nil {
		t.Fatal("empty notice id accepted")
	}
	if err := st.SetSetting(dismissedNoticesSetting, `["zeta","alpha","zeta"]`); err != nil {
		t.Fatal(err)
	}
	if err := st.DismissNotice("middle"); err != nil {
		t.Fatal(err)
	}
	if err := st.DismissNotice("alpha"); err != nil {
		t.Fatal(err)
	}
	raw, err := st.Setting(dismissedNoticesSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != `["alpha","middle","zeta"]` {
		t.Fatalf("dismissed notices = %s", raw)
	}
}

func TestDismissNoticeMalformedSettingDoesNotOverwriteIt(t *testing.T) {
	st := newTestStore(t)
	if err := st.SetSetting(dismissedNoticesSetting, `{broken`); err != nil {
		t.Fatal(err)
	}
	if err := st.DismissNotice("new"); err == nil {
		t.Fatal("malformed dismissed notices accepted")
	}
	raw, err := st.Setting(dismissedNoticesSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != `{broken` {
		t.Fatalf("malformed setting overwritten with %q", raw)
	}
}

func TestDismissNoticeMergesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	const managerCount = 4
	const noticesPerManager = 12
	stores := make([]*Store, managerCount)
	for i := range stores {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = st
		t.Cleanup(func() { _ = st.Close() })
	}

	start := make(chan struct{})
	errs := make(chan error, managerCount)
	var writers sync.WaitGroup
	for manager, st := range stores {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			for n := range noticesPerManager {
				if err := st.DismissNotice(fmt.Sprintf("manager-%d-notice-%02d", manager, n)); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	close(start)
	writers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	raw, err := stores[0].Setting(dismissedNoticesSetting)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode dismissed notices: %v", err)
	}
	want := make([]string, 0, managerCount*noticesPerManager)
	for manager := range managerCount {
		for n := range noticesPerManager {
			want = append(want, fmt.Sprintf("manager-%d-notice-%02d", manager, n))
		}
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("dismissed notices lost concurrent writes:\n got %q\nwant %q", got, want)
	}
}
