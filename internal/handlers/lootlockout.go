package handlers

import (
	"fmt"
	"log"
	"time"

	"github.com/eqoa/server/internal/database"
	"github.com/eqoa/server/internal/session"
)

func (h *NPCWindowHandler) lootLockoutRefusal(char uint32, lock database.LootLockout, now time.Time) string {
	if lock.Key == "" {
		return ""
	}
	until, err := h.inv.LootLockoutUntil(char, lock.Key)
	if err != nil {
		log.Printf("Loot lockout: character %d, %s: %v", char, lock.Key, err)
		return "This loot's lockout could not be checked. Try again shortly."
	}
	if now.Before(until) {
		return fmt.Sprintf("%s loot is locked for another %s.", lock.Name, lootLockoutRemaining(until.Sub(now)))
	}
	return ""
}

func warnLootLockout(sess *session.Session, lock database.LootLockout) {
	if lock.Key != "" {
		SendColoredChat(sess, fmt.Sprintf("Receiving this loot starts your %s %s loot lockout.",
			lootLockoutSpan(lock.Duration), lock.Name), raidR, raidG, raidB)
	}
}

func tellLootLockoutRefusal(sess *session.Session, line string) {
	if line != "" {
		SendColoredChat(sess, line, 255, 63, 63)
	}
}

func lootLockoutSpan(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%d-hour", d/time.Hour)
	}
	return spanWords(d)
}

func lootLockoutRemaining(d time.Duration) string {
	if d >= time.Hour {
		hours := d / time.Hour
		mins := d % time.Hour / time.Minute
		return fmt.Sprintf("%d hr %d min", hours, mins)
	}
	return spanWords(d)
}

// The stock Take All packet puts every visible record in the client's pack.
// Send only the awards the server actually made for a corpse with locked loot.
func lootLockoutStopsTakeAll(items []database.CharInventoryEntry) bool {
	for _, rec := range items {
		if lootLockoutFor(rec.Pattern.PatternID).Key != "" {
			return true
		}
	}
	return false
}

func (h *NPCWindowHandler) warnCorpseLootLockouts(sess *session.Session, char uint32, items []database.CharInventoryEntry, now time.Time) {
	seen := map[string]bool{}
	for _, rec := range items {
		lock := lootLockoutFor(rec.Pattern.PatternID)
		if lock.Key == "" || seen[lock.Key] {
			continue
		}
		seen[lock.Key] = true
		if why := h.lootLockoutRefusal(char, lock, now); why != "" {
			tellLootLockoutRefusal(sess, why)
		} else {
			warnLootLockout(sess, lock)
		}
	}
}
