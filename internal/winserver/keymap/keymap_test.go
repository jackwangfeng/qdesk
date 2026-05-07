package keymap

import "testing"

func TestParseSimpleLetterCombo(t *testing.T) {
	got, err := Parse("ctrl+f")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := []Event{
		{VK: VKControl, Down: true},
		{VK: 'F', Down: true},
		{VK: 'F', Down: false},
		{VK: VKControl, Down: false},
	}
	if !equalEvents(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestParseMultipleModifiers(t *testing.T) {
	got, err := Parse("ctrl+shift+a")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("expected 6 events, got %d: %+v", len(got), got)
	}
	if got[0].VK != VKControl || got[1].VK != VKShift || got[2].VK != 'A' {
		t.Errorf("modifier order wrong: %+v", got)
	}
	if !got[2].Down || got[3].Down {
		t.Errorf("A should be down then up: %+v", got[2:4])
	}
}

func TestParseWinKey(t *testing.T) {
	got, err := Parse("win+r")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got[0].VK != VKLWin {
		t.Errorf("first event should be VKLWin, got 0x%x", got[0].VK)
	}
}

func TestParseSingleKey(t *testing.T) {
	got, err := Parse("return")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("single key should be 2 events; got %+v", got)
	}
	if got[0].VK != VKReturn || !got[0].Down || got[1].Down {
		t.Errorf("return events wrong: %+v", got)
	}
}

func TestParseAltTab(t *testing.T) {
	got, err := Parse("alt+tab")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got[0].VK != VKMenu || got[1].VK != VKTab {
		t.Errorf("alt+tab parse wrong: %+v", got)
	}
}

func TestParseUnknownKey(t *testing.T) {
	_, err := Parse("ctrl+notakey")
	if err == nil {
		t.Errorf("expected error for unknown key")
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Errorf("expected error for empty combo")
	}
}

func equalEvents(a, b []Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
