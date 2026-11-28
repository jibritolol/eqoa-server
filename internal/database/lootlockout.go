package database

import (
	"database/sql"
	"errors"
	"time"
)

// LootLockout groups drops that share one claim per character for Duration.
// Use a stable Key, independent of the encounter instance and item pattern.
type LootLockout struct {
	Key, Name string
	Duration  time.Duration
}

var ErrLootLocked = errors.New("loot lockout is still active")

// LootLockoutUntil gives the last claim's expiry, or zero if there was no claim.
func (s *InventoryStore) LootLockoutUntil(serverID uint32, key string) (time.Time, error) {
	var expires int64
	err := s.db.QueryRow(`SELECT expires_ms FROM charlootlockout WHERE serverid = ? AND scope = ?`,
		serverID, key).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(expires), nil
}

// GrantLockedLoot claims a lockout and gives the item in the same transaction.
// For a stack merge, onto is its existing item ID and stack its resulting size.
func (s *InventoryStore) GrantLockedLoot(serverID uint32, rec CharInventoryEntry, count, onto, stack, location int,
	lock LootLockout, now time.Time) (int, error) {
	if lock.Key == "" || lock.Duration <= 0 || count <= 0 {
		return 0, errors.New("invalid loot lockout claim")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO charlootlockout (serverid, scope, expires_ms) VALUES (?, ?, ?)
		ON CONFLICT (serverid, scope) DO UPDATE SET expires_ms = excluded.expires_ms
		WHERE charlootlockout.expires_ms <= ?`, serverID, lock.Key, now.Add(lock.Duration).UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, err
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if claimed == 0 {
		return 0, ErrLootLocked
	}

	id := onto
	if onto != 0 {
		err = execInventoryRow(tx.Exec, `UPDATE charinventory SET stackleft = ?
			WHERE itemid = ? AND serverid = ? AND patternid = ? AND location = ? AND stackleft = ?`,
			stack, onto, serverID, rec.Pattern.PatternID, location, stack-count)
	} else {
		id, err = addCharItem(tx.Exec, serverID, rec.Pattern.PatternID, count, rec.RemainHP, rec.Charges, location)
	}
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
