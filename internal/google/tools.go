package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/tool"
)

// Tools returns the Google tool set (Calendar + Gmail) as eigen tools, grouped
// niche under "google" (progressive disclosure). They no-op with a clear "not
// connected / not configured" error until the user links their account, so the
// set is always safe to register. nowFn supplies the current time (overridable
// in tests); nil = time.Now.
func (a *Auth) Tools(nowFn func() time.Time) []tool.Definition {
	if nowFn == nil {
		nowFn = time.Now
	}
	const group = "google"
	const gist = "the user's Google Calendar + Gmail (read events/email, create events)"

	return []tool.Definition{
		{
			Name:        "google_calendar_list",
			Description: "List upcoming Google Calendar events. Args: {\"days\":7,\"calendar\":\"primary\"} — days ahead to look (default 7), calendar id (default primary).",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"days":{"type":"integer","description":"days ahead (default 7)"},"calendar":{"type":"string","description":"calendar id (default primary)"}}}`),
			ReadOnly:    true,
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "calendar", CapabilityDesc: "read + create Google Calendar events",
			Run: func(ctx context.Context, args json.RawMessage) (string, error) {
				return a.calendarList(ctx, args, nowFn)
			},
		},
		{
			Name:        "google_calendar_create",
			Description: "Create a Google Calendar event. Args: {\"summary\":\"Title\",\"start\":\"2026-07-01T15:00:00\",\"end\":\"2026-07-01T16:00:00\",\"timezone\":\"Asia/Jerusalem\",\"description\":\"\",\"location\":\"\",\"calendar\":\"primary\"}. start/end are local datetimes (RFC3339 without offset) or all-day dates (YYYY-MM-DD).",
			Parameters:  json.RawMessage(`{"type":"object","required":["summary","start","end"],"properties":{"summary":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"timezone":{"type":"string"},"description":{"type":"string"},"location":{"type":"string"},"calendar":{"type":"string"}}}`),
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "calendar", CapabilityDesc: "read + create Google Calendar events",
			Run: func(ctx context.Context, args json.RawMessage) (string, error) {
				return a.calendarCreate(ctx, args)
			},
		},
		{
			Name:        "gmail_list",
			Description: "List recent Gmail messages. Args: {\"query\":\"in:inbox\",\"max\":12} — Gmail search query (default in:inbox) and max messages (default 12).",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Gmail search (default in:inbox)"},"max":{"type":"integer","description":"max messages (default 12)"}}}`),
			ReadOnly:    true,
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "gmail", CapabilityDesc: "read recent Gmail messages",
			Run: func(ctx context.Context, args json.RawMessage) (string, error) {
				return a.gmailList(ctx, args)
			},
		},
	}
}

// ── Calendar ────────────────────────────────────────────────────────────────

func (a *Auth) calendarList(ctx context.Context, args json.RawMessage, nowFn func() time.Time) (string, error) {
	var in struct {
		Days     int    `json:"days"`
		Calendar string `json:"calendar"`
	}
	_ = json.Unmarshal(args, &in)
	if in.Days <= 0 {
		in.Days = 7
	}
	cal := in.Calendar
	if cal == "" {
		cal = "primary"
	}
	now := nowFn()
	timeMin := now.Format(time.RFC3339)
	timeMax := now.Add(time.Duration(in.Days) * 24 * time.Hour).Format(time.RFC3339)
	u := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events?singleEvents=true&orderBy=startTime&maxResults=50&timeMin=%s&timeMax=%s",
		url.PathEscape(cal), url.QueryEscape(timeMin), url.QueryEscape(timeMax))

	var resp struct {
		Items []struct {
			Summary  string `json:"summary"`
			Location string `json:"location"`
			Start    struct {
				DateTime string `json:"dateTime"`
				Date     string `json:"date"`
			} `json:"start"`
			End struct {
				DateTime string `json:"dateTime"`
				Date     string `json:"date"`
			} `json:"end"`
		} `json:"items"`
	}
	if err := a.getJSON(ctx, u, &resp); err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return fmt.Sprintf("No events in the next %d days.", in.Days), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Upcoming events (next %d days):\n", in.Days)
	for _, e := range resp.Items {
		when := e.Start.DateTime
		if when == "" {
			when = e.Start.Date + " (all day)"
		}
		title := e.Summary
		if title == "" {
			title = "(no title)"
		}
		fmt.Fprintf(&b, "- %s — %s", when, title)
		if e.Location != "" {
			fmt.Fprintf(&b, " @ %s", e.Location)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (a *Auth) calendarCreate(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Summary     string `json:"summary"`
		Start       string `json:"start"`
		End         string `json:"end"`
		Timezone    string `json:"timezone"`
		Description string `json:"description"`
		Location    string `json:"location"`
		Calendar    string `json:"calendar"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if in.Summary == "" || in.Start == "" || in.End == "" {
		return "", fmt.Errorf("summary, start, and end are required")
	}
	cal := in.Calendar
	if cal == "" {
		cal = "primary"
	}
	// All-day when start looks like YYYY-MM-DD (10 chars, no 'T').
	timeField := func(v string) map[string]string {
		if len(v) == 10 && !strings.Contains(v, "T") {
			return map[string]string{"date": v}
		}
		m := map[string]string{"dateTime": v}
		if in.Timezone != "" {
			m["timeZone"] = in.Timezone
		}
		return m
	}
	body := map[string]any{
		"summary": in.Summary,
		"start":   timeField(in.Start),
		"end":     timeField(in.End),
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Location != "" {
		body["location"] = in.Location
	}
	u := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events", url.PathEscape(cal))
	var out struct {
		ID      string `json:"id"`
		HTMLink string `json:"htmlLink"`
	}
	if err := a.postJSON(ctx, u, body, &out); err != nil {
		return "", err
	}
	link := out.HTMLink
	if link == "" {
		link = "(no link)"
	}
	return fmt.Sprintf("Created event %q — %s", in.Summary, link), nil
}

// ── Gmail ─────────────────────────────────────────────────────────────────

func (a *Auth) gmailList(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
		Max   int    `json:"max"`
	}
	_ = json.Unmarshal(args, &in)
	if in.Query == "" {
		in.Query = "in:inbox"
	}
	if in.Max <= 0 || in.Max > 50 {
		in.Max = 12
	}
	listURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages?q=%s&maxResults=%d",
		url.QueryEscape(in.Query), in.Max)
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := a.getJSON(ctx, listURL, &list); err != nil {
		return "", err
	}
	if len(list.Messages) == 0 {
		return fmt.Sprintf("No messages for %q.", in.Query), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recent messages (%s):\n", in.Query)
	for _, m := range list.Messages {
		msgURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date", url.PathEscape(m.ID))
		var msg struct {
			Snippet string `json:"snippet"`
			Payload struct {
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"payload"`
		}
		if err := a.getJSON(ctx, msgURL, &msg); err != nil {
			continue // skip a message that fails to fetch; keep the rest
		}
		from, subject := "", ""
		for _, h := range msg.Payload.Headers {
			switch h.Name {
			case "From":
				from = h.Value
			case "Subject":
				subject = h.Value
			}
		}
		if subject == "" {
			subject = "(no subject)"
		}
		fmt.Fprintf(&b, "- %s — %s\n", from, subject)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
