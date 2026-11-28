package handlers

import (
	"fmt"
	"log"
	"time"

	"github.com/eqoa/server/internal/database"
	"github.com/eqoa/server/internal/entity"
	"github.com/eqoa/server/internal/session"
)

// A first try at loot that starts a lockout takes nothing and says what it
// costs; another takes it, from lootLockoutAskLeast (so a double press is not
// an answer) to lootLockoutAskFor after the warning.
const (
	lootLockoutAskLeast = time.Second
	lootLockoutAskFor   = 30 * time.Second
)

// lootLockoutAsk is a character's warning over a corpse's locked loot.
type lootLockoutAsk struct {
	char   uint32
	corpse entity.EntityID
	key    string
}

// lootLockoutAgreedLocked reports whether the character may take loot under
// lock now, warning it when it has not been. Must be called with ls.mu held.
func (ls *lootState) lootLockoutAgreedLocked(sess *session.Session, char uint32, corpse entity.EntityID,
	lock database.LootLockout, now time.Time) bool {
	if lock.Key == "" {
		return true
	}
	for a, at := range ls.asked {
		if now.Sub(at) > lootLockoutAskFor {
			delete(ls.asked, a)
		}
	}
	ask := lootLockoutAsk{char, corpse, lock.Key}
	if at, ok := ls.asked[ask]; ok {
		if now.Sub(at) < lootLockoutAskLeast {
			return false // the same press, or the one after it
		}
		delete(ls.asked, ask)
		return true
	}
	if ls.asked == nil {
		ls.asked = map[lootLockoutAsk]time.Time{}
	}
	ls.asked[ask] = now
	SendColoredChat(sess, fmt.Sprintf("Taking this starts your %s %s loot lockout. Loot it again within %d seconds to take it.",
		lootLockoutSpan(lock.Duration), lock.Name, int(lootLockoutAskFor/time.Second)), raidR, raidG, raidB)
	return false
}

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
