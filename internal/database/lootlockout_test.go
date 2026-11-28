package database

import (
	"errors"
	"testing"
	"time"
)

func TestLootLockoutClaimsAndInventoryCommitTogether(t *testing.T) {
	db, inv, char, _ := buyBackDB(t)
	pattern := buyBackPattern(t, db, "Test Mark", 1)
	if _, err := db.Exec(`UPDATE itempattern SET maxstack = 99 WHERE patternid = ?`, pattern); err != nil {
		t.Fatal(err)
	}
	rec := *buyBackRow(pattern, 1, 50, 0)
	lock := LootLockout{Key: "test:boss", Name: "Test Boss", Duration: 18 * time.Hour}
	now := time.UnixMilli(1_800_000_000_000)

	if _, err := inv.GrantLockedLoot(char, rec, 1, 1000000, 2, 0, lock, now); err == nil {
		t.Fatal("a write to a missing inventory row succeeded")
	}
	if expires, err := inv.LootLockoutUntil(char, lock.Key); err != nil || !expires.IsZero() {
		t.Fatalf("failed grant started a lockout until %v (%v)", expires, err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			_, err := inv.GrantLockedLoot(char, rec, 1, 0, 0, 0, lock, now)
			results <- err
		}()
	}
	close(start)
	granted := 0
	for i := 0; i < cap(results); i++ {
		switch err := <-results; {
		case err == nil:
			granted++
		case !errors.Is(err, ErrLootLocked):
			t.Fatal(err)
		}
	}
	if granted != 1 {
		t.Fatalf("%d grants succeeded, want one", granted)
	}

	reopened, err := NewSQLite(db.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	inv = NewInventoryStore(reopened)
	expires := now.Add(lock.Duration)
	if got, err := inv.LootLockoutUntil(char, lock.Key); err != nil || !got.Equal(expires) {
		t.Fatalf("reopened lockout %v, want %v (%v)", got, expires, err)
	}
	items, err := inv.GetPackItems(char)
	if err != nil || len(items) != 1 || items[0].StackLeft != 1 {
		t.Fatalf("pack after one grant: %+v (%v)", items, err)
	}
	if _, err := inv.GrantLockedLoot(char, rec, 1, items[0].ItemID, 2, 0, lock, expires.Add(-time.Millisecond)); !errors.Is(err, ErrLootLocked) {
		t.Fatalf("a claim just before expiry gave %v", err)
	}
	if _, err := inv.GrantLockedLoot(char, rec, 1, items[0].ItemID, 2, 0, lock, expires); err != nil {
		t.Fatal(err)
	}
	other := LootLockout{Key: "test:other", Name: "Other Boss", Duration: time.Hour}
	if _, err := inv.GrantLockedLoot(char, rec, 1, items[0].ItemID, 3, 0, other, expires); err != nil {
		t.Fatalf("a different lockout shares the first cooldown: %v", err)
	}
	items, err = inv.GetPackItems(char)
	if err != nil || len(items) != 1 || items[0].StackLeft != 3 {
		t.Fatalf("pack after valid stack claims: %+v (%v)", items, err)
	}
}
