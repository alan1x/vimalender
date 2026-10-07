package internal

import (
	"strings"
	"testing"
	"time"
)

func testEvent(date string, startMin int, id string) Event {
	d, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	return Event{ID: id, Title: "Clase " + id, Date: d, DateStr: date, StartMin: startMin, EndMin: startMin + 75}
}

func TestNotifyUpcoming(t *testing.T) {
	today := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local) // Thursday
	tomorrow := today.AddDate(0, 0, 1)
	store := NewEventStore()
	store.events[today] = []Event{testEvent("2026-10-08", 600, "a")}     // 10:00
	store.events[tomorrow] = []Event{testEvent("2026-10-09", 30, "b")}   // 00:30 (madrugada)
	leads := []int{60, 20}
	none := func(string) bool { return false }

	// 09:00 → se cruza el umbral de 60 min
	got := upcomingNotifications(store, today.Add(9*time.Hour), leads, 3*time.Minute, time.Minute, none)
	if len(got) != 1 || got[0].LeadMin != 60 || got[0].Title != "Clase a" {
		t.Fatalf("09:00 → %+v", got)
	}
	// 09:40 → se cruza el umbral de 20 min
	got = upcomingNotifications(store, today.Add(9*time.Hour+40*time.Minute), leads, 3*time.Minute, time.Minute, none)
	if len(got) != 1 || got[0].LeadMin != 20 {
		t.Fatalf("09:40 → %+v", got)
	}
	// 09:05 → fuera del lookback, nada
	got = upcomingNotifications(store, today.Add(9*time.Hour+5*time.Minute), leads, 3*time.Minute, time.Minute, none)
	if len(got) != 0 {
		t.Fatalf("09:05 → %+v", got)
	}
	// ya notificado (dedupe)
	got = upcomingNotifications(store, today.Add(9*time.Hour), leads, 3*time.Minute, time.Minute, func(string) bool { return true })
	if len(got) != 0 {
		t.Fatalf("dedupe → %+v", got)
	}
	// evento ya iniciado (gracia): umbral dentro del lookback pero start viejo → se salta
	nowLate := today.Add(10*time.Hour + 2*time.Minute) // 10:02
	got = upcomingNotifications(store, nowLate, []int{3}, 10*time.Minute, time.Minute, none)
	if len(got) != 0 {
		t.Fatalf("gracia → %+v", got)
	}
	// medianoche: a las 23:31, evento de mañana 00:30 cruza su umbral de 60 min (23:30)
	got = upcomingNotifications(store, today.Add(23*time.Hour+31*time.Minute), leads, 3*time.Minute, time.Minute, none)
	if len(got) != 1 || got[0].LeadMin != 60 {
		t.Fatalf("medianoche → %+v", got)
	}
	if !strings.Contains(got[0].Body, "mañana") || !strings.Contains(got[0].Body, "en 1 h") {
		t.Fatalf("body → %q", got[0].Body)
	}
}
