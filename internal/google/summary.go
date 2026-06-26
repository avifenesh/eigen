package google

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Structured summaries for the working-station dashboard (distinct from the
// agent tools in tools.go, which return prose). These return typed data the GUI
// renders directly. All degrade to a clear error when not connected/configured;
// the dashboard treats that as "Google not linked" rather than a failure.

// CalEvent is one upcoming calendar event for the dashboard.
type CalEvent struct {
	Summary  string `json:"summary"`
	Start    string `json:"start"`    // RFC3339 dateTime, or YYYY-MM-DD for all-day
	AllDay   bool   `json:"allDay"`
	Location string `json:"location"`
}

// MailMsg is one recent message header for the dashboard.
type MailMsg struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
}

// UpcomingEvents returns events starting within the next `days` days (capped),
// soonest first.
func (a *Auth) UpcomingEvents(ctx context.Context, days, max int) ([]CalEvent, error) {
	if days <= 0 {
		days = 1
	}
	if max <= 0 || max > 20 {
		max = 8
	}
	now := time.Now()
	u := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/primary/events?singleEvents=true&orderBy=startTime&maxResults=%d&timeMin=%s&timeMax=%s",
		max, url.QueryEscape(now.Format(time.RFC3339)), url.QueryEscape(now.Add(time.Duration(days)*24*time.Hour).Format(time.RFC3339)))
	var resp struct {
		Items []struct {
			Summary  string `json:"summary"`
			Location string `json:"location"`
			Start    struct {
				DateTime string `json:"dateTime"`
				Date     string `json:"date"`
			} `json:"start"`
		} `json:"items"`
	}
	if err := a.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	out := make([]CalEvent, 0, len(resp.Items))
	for _, e := range resp.Items {
		ev := CalEvent{Summary: e.Summary, Location: e.Location}
		if ev.Summary == "" {
			ev.Summary = "(no title)"
		}
		if e.Start.DateTime != "" {
			ev.Start = e.Start.DateTime
		} else {
			ev.Start = e.Start.Date
			ev.AllDay = true
		}
		out = append(out, ev)
	}
	return out, nil
}

// UnreadCount returns the number of unread inbox messages (Gmail label INBOX).
func (a *Auth) UnreadCount(ctx context.Context) (int, error) {
	var resp struct {
		MessagesUnread int `json:"messagesUnread"`
	}
	if err := a.getJSON(ctx, "https://gmail.googleapis.com/gmail/v1/users/me/labels/INBOX", &resp); err != nil {
		return 0, err
	}
	return resp.MessagesUnread, nil
}

// RecentUnread returns up to `max` recent unread message headers for the
// dashboard.
func (a *Auth) RecentUnread(ctx context.Context, max int) ([]MailMsg, error) {
	if max <= 0 || max > 10 {
		max = 5
	}
	listURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages?q=%s&maxResults=%d",
		url.QueryEscape("in:inbox is:unread"), max)
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := a.getJSON(ctx, listURL, &list); err != nil {
		return nil, err
	}
	out := make([]MailMsg, 0, len(list.Messages))
	for _, m := range list.Messages {
		msgURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=Subject", url.PathEscape(m.ID))
		var msg struct {
			Payload struct {
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"payload"`
		}
		if err := a.getJSON(ctx, msgURL, &msg); err != nil {
			continue
		}
		var mm MailMsg
		for _, h := range msg.Payload.Headers {
			switch h.Name {
			case "From":
				mm.From = h.Value
			case "Subject":
				mm.Subject = h.Value
			}
		}
		if mm.Subject == "" {
			mm.Subject = "(no subject)"
		}
		out = append(out, mm)
	}
	return out, nil
}
