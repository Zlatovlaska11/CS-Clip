package parser

import (
	"fmt"
	"io"
	"os"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v3/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v3/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v3/pkg/demoinfocs/events"
)

// KillEvent is a single kill extracted from the demo.
type KillEvent struct {
	Tick          int
	TimeSeconds   float64
	KillerName    string
	KillerSteamID uint64
	VictimName    string
	Weapon        string
	IsHeadshot    bool
}

// DemoInfo contains metadata and all kill events parsed from a .dem file.
type DemoInfo struct {
	Path      string
	MapName   string
	Tickrate  float64
	Duration  time.Duration
	Kills     []KillEvent
}

// ParseDemo opens a .dem file and extracts kill events.
func ParseDemo(path string) (*DemoInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	return ParseDemoReader(path, f)
}

// ParseDemoReader parses from an io.Reader (useful for testing).
func ParseDemoReader(name string, r io.Reader) (*DemoInfo, error) {
	p := demoinfocs.NewParser(r)
	defer p.Close()

	info := &DemoInfo{Path: name}

	p.RegisterEventHandler(func(e events.Kill) {
		gs := p.GameState()
		tick := gs.IngameTick()

		tickrate := p.TickRate()
		if tickrate == 0 {
			tickrate = 64
		}
		secs := float64(tick) / tickrate

		ke := KillEvent{
			Tick:        tick,
			TimeSeconds: secs,
			VictimName:  safePlayerName(e.Victim),
			Weapon:      safeWeaponName(e.Weapon),
			IsHeadshot:  e.IsHeadshot,
		}
		if e.Killer != nil {
			ke.KillerName    = e.Killer.Name
			ke.KillerSteamID = e.Killer.SteamID64
		}

		info.Kills = append(info.Kills, ke)
	})

	if err := p.ParseToEnd(); err != nil {
		return nil, fmt.Errorf("parse demo: %w", err)
	}

	hdr := p.Header()
	info.MapName = hdr.MapName
	info.Tickrate = p.TickRate()
	if info.Tickrate == 0 {
		info.Tickrate = 64
	}
	info.Duration = hdr.PlaybackTime

	return info, nil
}

// PrintEvents dumps parsed kill events to stdout (for development verification).
func PrintEvents(info *DemoInfo) {
	fmt.Printf("Demo:     %s\n", info.Path)
	fmt.Printf("Map:      %s\n", info.MapName)
	fmt.Printf("Tickrate: %.0f\n", info.Tickrate)
	fmt.Printf("Duration: %s\n", info.Duration.Round(time.Second))
	fmt.Printf("Kills:    %d\n\n", len(info.Kills))

	for i, k := range info.Kills {
		hs := ""
		if k.IsHeadshot {
			hs = " [HS]"
		}
		fmt.Printf(
			"[%4d] tick=%-7d t=%7.1fs  %-24s killed %-24s with %s%s\n",
			i+1, k.Tick, k.TimeSeconds,
			k.KillerName, k.VictimName, k.Weapon, hs,
		)
	}
}

func safePlayerName(p *common.Player) string {
	if p == nil {
		return "<unknown>"
	}
	return p.Name
}

func safeWeaponName(w *common.Equipment) string {
	if w == nil {
		return "unknown"
	}
	return w.String()
}
