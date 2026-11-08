package highlights

import (
	"fmt"
	"sort"

	"zlatovlas/cs-clip/parser"
)

// Highlight represents a detected highlight moment with tick range and contributing kills.
type Highlight struct {
	ID          int
	Type        string // "multikill", "clutch" (future)
	PlayerName  string
	SteamID     uint64
	StartTick   int
	EndTick     int
	Kills       []parser.KillEvent
	KillCount   int
}

func (h Highlight) String() string {
	return fmt.Sprintf("%s by %s (%d kills, ticks %d-%d)",
		h.Type, h.PlayerName, h.KillCount, h.StartTick, h.EndTick)
}

// Detect runs all detectors against parsed demo info and returns highlights.
func Detect(info *parser.DemoInfo) []Highlight {
	var out []Highlight
	out = append(out, detectMultiKills(info)...)
	// future: out = append(out, detectClutches(info)...)
	for i := range out {
		out[i].ID = i + 1
	}
	return out
}

// detectMultiKills finds 2+ kills by the same player within 10 seconds.
func detectMultiKills(info *parser.DemoInfo) []Highlight {
	const (
		windowSecs   = 10.0
		prePadSecs   = 5.0
		postPadSecs  = 3.0
		minKills     = 2
	)

	tickrate := info.Tickrate
	if tickrate == 0 {
		tickrate = 64
	}

	// Group kills by killer steamID.
	byPlayer := map[uint64][]parser.KillEvent{}
	for _, k := range info.Kills {
		if k.KillerSteamID == 0 {
			continue
		}
		byPlayer[k.KillerSteamID] = append(byPlayer[k.KillerSteamID], k)
	}

	var highlights []Highlight

	for steamID, kills := range byPlayer {
		sort.Slice(kills, func(i, j int) bool {
			return kills[i].Tick < kills[j].Tick
		})

		// Sliding window to group kills within windowSecs.
		i := 0
		for i < len(kills) {
			j := i + 1
			for j < len(kills) {
				span := kills[j].TimeSeconds - kills[i].TimeSeconds
				if span <= windowSecs {
					j++
				} else {
					break
				}
			}
			group := kills[i:j]
			if len(group) >= minKills {
				first := group[0]
				last := group[len(group)-1]

				startTick := first.Tick - int(prePadSecs*tickrate)
				if startTick < 0 {
					startTick = 0
				}
				endTick := last.Tick + int(postPadSecs*tickrate)

				highlights = append(highlights, Highlight{
					Type:       "multikill",
					PlayerName: first.KillerName,
					SteamID:    steamID,
					StartTick:  startTick,
					EndTick:    endTick,
					Kills:      append([]parser.KillEvent(nil), group...),
					KillCount:  len(group),
				})
				i = j
			} else {
				i++
			}
		}
	}

	// Sort by start tick for deterministic output.
	sort.Slice(highlights, func(i, j int) bool {
		return highlights[i].StartTick < highlights[j].StartTick
	})
	return highlights
}
