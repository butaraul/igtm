package engine

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
)

// EventKind says who is speaking in the log.
type EventKind string

// Event kinds.
const (
	EvBrief   EventKind = "brief"
	EvClient  EventKind = "client"
	EvAgent   EventKind = "agent"
	EvTests   EventKind = "tests"
	EvExplain EventKind = "explain"
	EvYou     EventKind = "you"
	EvSystem  EventKind = "system"
)

// Event is one entry in the run's log.
type Event struct {
	Kind  EventKind
	Clock int  // minute it happened
	Seq   int  // candidate it concerns, or the last one presented
	Pass  bool // for EvTests
	Text  string
}

func (g *Game) logf(k EventKind, format string, args ...any) {
	g.log = append(g.log, Event{Kind: k, Clock: g.clock, Seq: len(g.cands), Text: fmt.Sprintf(format, args...)})
}

func (g *Game) logText(k EventKind, text string) {
	g.log = append(g.log, Event{Kind: k, Clock: g.clock, Seq: len(g.cands), Text: text})
}

// roll is a deterministic number in [0, 1) derived from the seed and parts.
// Each decision rolls its own number, so one choice never shifts another.
func (g *Game) roll(parts ...string) float64 {
	h := fnv.New64a()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], g.cfg.Seed)
	h.Write(b[:])
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	x := h.Sum64()
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return float64(x>>11) / (1 << 53)
}

// FormatClock renders working minutes as a day and time. Days run 09:00 to
// 17:00.
func FormatClock(m int) string {
	const day = 8 * 60
	t := 9*60 + m%day
	return fmt.Sprintf("Day %d %02d:%02d", m/day+1, t/60, t%60)
}

// FormatDuration renders minutes as 3h05m or 45m.
func FormatDuration(m int) string {
	if m < 0 {
		m = 0
	}
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}
