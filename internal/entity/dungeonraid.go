package entity

import (
	"fmt"
	"strings"
	"time"

	"github.com/eqoa/server/internal/session"
)

const HeroicRaidBoardName = "Heroic Raids"
const heroicRaidCap = 10

// raidEntryHold is how long a reserved seat waits for its player to arrive.
const raidEntryHold = 10 * time.Second

func (d *DungeonDef) heroicRaid() bool {
	return d.Encounter != "" || strings.EqualFold(d.Via, HeroicRaidBoardName)
}

// raidOccupantsLocked counts characters already inside and arrivals queued by
// the door. A dead player still on the island occupies their seat.
func (w *World) raidOccupantsLocked(in *dungeonInst) map[uint32]bool {
	world := w.dungeons.defs[in.def].World
	chars := make(map[uint32]bool)
	for _, p := range w.Players {
		if p.Instance == in.id && (p.World == world || p.raidEntry == in.id) {
			chars[p.CharServerID] = true
		}
	}
	return chars
}

func (w *World) raidPullActiveLocked(in *dungeonInst) bool {
	f := in.enc
	if f == nil || f.completed {
		return false
	}
	boss := w.NPCs[f.boss]
	return boss != nil && !boss.Dead &&
		(f.phase != mythicStaging || boss.CurrentEnemy != 0 || len(boss.Threats) != 0)
}

func (w *World) raidEntryRefusalLocked(in *dungeonInst, char uint32) string {
	d := &w.dungeons.defs[in.def]
	if !d.heroicRaid() {
		return ""
	}
	if w.raidPullActiveLocked(in) {
		roster := in.raidPull
		if roster == nil { // pulled, and the fight's next step saves its roster
			roster = w.raidOccupantsLocked(in)
		}
		if !roster[char] {
			return fmt.Sprintf("%s is in progress. New players can enter after the pull ends.", d.Name)
		}
	}
	chars := w.raidOccupantsLocked(in)
	if !chars[char] && len(chars) >= heroicRaidCap {
		return fmt.Sprintf("%s already has %d players. Wait for a spot to open.", d.Name, heroicRaidCap)
	}
	return ""
}

func (w *World) setDungeonInstanceLocked(p *Player, in *dungeonInst) {
	p.Instance = in.id
	p.raidEntry, p.raidEntrySeen = 0, time.Time{}
	d := &w.dungeons.defs[in.def]
	if d.heroicRaid() && p.World != d.World {
		p.raidEntry = in.id
	}
}

// A rez returns only to the copy where the character died. Its frozen roster
// still includes that character after a respawn outside the raid.
func (w *World) reserveRaidReturnLocked(sess *session.Session, p *Player, world uint8, inst uint32) bool {
	raidWorld := false
	for i := range w.dungeons.defs {
		d := &w.dungeons.defs[i]
		if d.World == world && d.heroicRaid() {
			raidWorld = true
			break
		}
	}
	if !raidWorld {
		return true
	}
	if inst == 0 && p.World == world {
		inst = p.Instance // a rez saved before its instance was recorded
	}
	in := w.dungeons.insts[inst]
	if in == nil || w.dungeons.defs[in.def].World != world || !w.dungeons.defs[in.def].heroicRaid() ||
		!in.stood[p.CharServerID] || !in.removed[p.CharServerID].IsZero() {
		w.sendColoredLocked(sess, "You can no longer return to the raid instance where you died.", castRefusalRGB)
		return false
	}
	if why := w.raidEntryRefusalLocked(in, p.CharServerID); why != "" {
		w.sendColoredLocked(sess, why, castRefusalRGB)
		return false
	}
	w.setDungeonInstanceLocked(p, in)
	return true
}

// FinishRaidEntry releases a seat if a queued world transfer failed.
func (w *World) FinishRaidEntry(id EntityID, inst uint32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p := w.Players[id]; p != nil && p.raidEntry == inst {
		p.raidEntry = 0
		if p.Instance == inst {
			p.Instance = 0
		}
	}
}

// raidSeatSettleLocked lets go of a seat reserved for a player on its way in:
// once it stands in the raid, or after raidEntryHold if it never arrived. A
// GM's teleport, a board's way back and Return Home move a player without
// place, so a seat left to raidPlacementLocked alone could stay taken.
func (w *World) raidSeatSettleLocked(p *Player, now time.Time) {
	if p.raidEntry == 0 {
		return
	}
	in := w.dungeons.insts[p.raidEntry]
	if in != nil && (p.Instance != in.id || p.World != w.dungeons.defs[in.def].World) {
		if p.raidEntrySeen.IsZero() {
			p.raidEntrySeen = now
		}
		if now.Sub(p.raidEntrySeen) < raidEntryHold {
			return
		}
		if p.Instance == in.id {
			p.Instance = 0
		}
	}
	p.raidEntry, p.raidEntrySeen = 0, time.Time{}
}

// raidPlacementLocked keeps a raid instance ID only on its own map. A player
// arriving from another map first needs a seat from the door, summon or rez.
func (w *World) raidPlacementLocked(p *Player, world uint8) {
	if in := w.dungeons.insts[p.Instance]; in != nil {
		d := &w.dungeons.defs[in.def]
		if d.heroicRaid() {
			switch {
			case p.World == d.World || p.raidEntry == in.id:
				if world != d.World {
					p.Instance = 0
				}
			case world == d.World:
				p.Instance = 0
			}
		}
	}
	p.raidEntry = 0
}
