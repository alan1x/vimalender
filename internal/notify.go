package internal

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// pendingNotification is one alert that is due now.
type pendingNotification struct {
	Key     string
	Title   string
	Body    string
	Start   time.Time
	LeadMin int
}

// notifyState keeps track of alerts already sent so each threshold fires once.
type notifyState struct {
	Keys map[string]string `json:"keys"` // event-instance key -> RFC3339 time notified
}

func notifyStatePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "vimalender", "notified.json")
}

func loadNotifyState(path string) *notifyState {
	if path == "" {
		path = notifyStatePath()
	}
	st := &notifyState{Keys: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, st); err != nil || st.Keys == nil {
		return &notifyState{Keys: map[string]string{}}
	}
	return st
}

func (s *notifyState) save(path string) error {
	if path == "" {
		path = notifyStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *notifyState) prune(now time.Time) {
	cutoff := now.AddDate(0, 0, -4)
	for k, v := range s.Keys {
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.Before(cutoff) {
			delete(s.Keys, k)
		}
	}
}

// parseLeads parses "60,20" into minutes, largest first.
func parseLeads(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid lead %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no lead times given")
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out, nil
}

func humanizeLead(min int) string {
	if min%60 == 0 {
		if h := min / 60; h == 1 {
			return "1 h"
		} else {
			return fmt.Sprintf("%d h", h)
		}
	}
	if min > 60 {
		return fmt.Sprintf("%d h %d min", min/60, min%60)
	}
	return fmt.Sprintf("%d min", min)
}

// upcomingNotifications returns the alerts due now: for each event on today and
// tomorrow, every lead whose threshold (start-lead) was crossed within the
// lookback window, skipping events that already started (beyond grace) and
// thresholds already notified.
func upcomingNotifications(store *EventStore, now time.Time, leads []int, lookback, grace time.Duration, seen func(string) bool) []pendingNotification {
	today := DateKey(now)
	var out []pendingNotification
	for _, day := range []time.Time{today, today.AddDate(0, 0, 1)} {
		for _, ev := range store.GetByDate(day) {
			start := ev.Date.Add(time.Duration(ev.StartMin) * time.Minute)
			for _, lead := range leads {
				threshold := start.Add(-time.Duration(lead) * time.Minute)
				if threshold.After(now) || !threshold.After(now.Add(-lookback)) {
					continue
				}
				if start.Before(now.Add(-grace)) {
					continue // event already started: stale alert
				}
				key := fmt.Sprintf("%s|%s|%d|%d", ev.ID, day.Format("2006-01-02"), ev.StartMin, lead)
				if seen(key) {
					continue
				}
				out = append(out, pendingNotification{
					Key:     key,
					Title:   ev.Title,
					Body:    notifyBody(ev, start, now, lead),
					Start:   start,
					LeadMin: lead,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].LeadMin > out[j].LeadMin
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out
}

func notifyBody(ev Event, start, now time.Time, lead int) string {
	when := fmt.Sprintf("%s–%s", MinToTime(ev.StartMin), MinToTime(ev.EndMin))
	if DateKey(start) != DateKey(now) {
		when = "mañana " + when
	}
	parts := []string{when}
	if ev.Desc != "" {
		parts = append(parts, ev.Desc)
	}
	parts = append(parts, "en "+humanizeLead(lead))
	return strings.Join(parts, " · ")
}

func sendNotification(p pendingNotification) error {
	return exec.Command("notify-send", "-a", "vimalender", "-u", "normal", p.Title, p.Body).Run()
}

// RunNotify implements the `vimalender notify` subcommand: it checks for
// upcoming events and sends desktop notifications for each due lead time.
func RunNotify(args []string) int {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	leadFlag := fs.String("lead", "60,20", "lead times in minutes, comma separated (e.g. 60,20)")
	lookback := fs.Duration("lookback", 3*time.Minute, "how long after crossing a threshold the alert still fires")
	grace := fs.Duration("grace", time.Minute, "skip alerts for events that started longer ago than this")
	eventsPath := fs.String("events", "", "override events.json path (testing)")
	statePath := fs.String("state", "", "override state file path (testing)")
	dryRun := fs.Bool("dry-run", false, "print due alerts without sending notifications or saving state")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	leads, err := parseLeads(*leadFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vimalender notify:", err)
		return 2
	}

	var store *EventStore
	var errMsg string
	if *eventsPath != "" {
		store, errMsg = loadEventsFile(*eventsPath)
	} else {
		store, errMsg = LoadEvents()
	}
	if errMsg != "" {
		fmt.Fprintln(os.Stderr, "vimalender notify:", errMsg)
	}
	if store == nil {
		return 1
	}

	state := loadNotifyState(*statePath)
	now := time.Now()
	due := upcomingNotifications(store, now, leads, *lookback, *grace, func(k string) bool {
		_, ok := state.Keys[k]
		return ok
	})
	if *dryRun && len(due) == 0 {
		fmt.Println("(sin alertas pendientes)")
	}
	for _, p := range due {
		if *dryRun {
			fmt.Printf("[dry-run] %s | %s\n", p.Title, p.Body)
			continue
		}
		if err := sendNotification(p); err != nil {
			fmt.Fprintf(os.Stderr, "vimalender notify: %s: %v\n", p.Title, err)
			continue // not marked: retried on the next run
		}
		state.Keys[p.Key] = now.Format(time.RFC3339)
	}
	if !*dryRun {
		state.prune(now)
		if err := state.save(*statePath); err != nil {
			fmt.Fprintln(os.Stderr, "vimalender notify: save state:", err)
			return 1
		}
	}
	return 0
}
