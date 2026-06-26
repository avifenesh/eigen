package connector

import "strings"

// Catalog is the curated directory of known remote-MCP connectors — a
// one-click "browse & connect" list over the generic OAuth flow. Each entry is
// just a friendly name + the server's documented remote MCP URL; connecting one
// runs the SAME dynamic-discovery + PKCE flow as a hand-typed URL, so the
// directory is pure UX sugar (no per-vendor auth code). URLs are the vendors'
// published Streamable-HTTP endpoints; a user can still add any URL by hand.

// CatalogEntry is one curated connector.
type CatalogEntry struct {
	// Name is the stable connector id (also the mcp.json server name).
	Name string `json:"name"`
	// Display is the human label ("Notion", "Linear").
	Display string `json:"display"`
	// Glyph is a short emoji/char shown in the grid tile (no icon assets to ship).
	Glyph string `json:"glyph"`
	// URL is the server's remote MCP endpoint.
	URL string `json:"url"`
	// Description is a one-line "what it does".
	Description string `json:"description"`
	// Category groups tiles ("Docs & notes", "Dev", "Comms", …).
	Category string `json:"category"`
}

// catalog is the built-in connector directory. Kept small + explicit; extend by
// adding entries. Ordered by category then name for stable rendering.
var catalog = []CatalogEntry{
	{Name: "notion", Display: "Notion", Glyph: "📝", URL: "https://mcp.notion.com/mcp", Description: "Read & write Notion pages and databases.", Category: "Docs & notes"},
	{Name: "linear", Display: "Linear", Glyph: "📐", URL: "https://mcp.linear.app/mcp", Description: "Issues, projects, and cycles in Linear.", Category: "Project management"},
	{Name: "asana", Display: "Asana", Glyph: "✅", URL: "https://mcp.asana.com/sse", Description: "Tasks and projects in Asana.", Category: "Project management"},
	{Name: "atlassian", Display: "Atlassian", Glyph: "🧩", URL: "https://mcp.atlassian.com/v1/sse", Description: "Jira issues and Confluence pages.", Category: "Project management"},
	{Name: "sentry", Display: "Sentry", Glyph: "🛡️", URL: "https://mcp.sentry.dev/mcp", Description: "Errors, issues, and releases in Sentry.", Category: "Dev"},
	{Name: "stripe", Display: "Stripe", Glyph: "💳", URL: "https://mcp.stripe.com", Description: "Payments, customers, and invoices in Stripe.", Category: "Business"},
	{Name: "intercom", Display: "Intercom", Glyph: "💬", URL: "https://mcp.intercom.com/mcp", Description: "Conversations and contacts in Intercom.", Category: "Comms"},
	{Name: "paypal", Display: "PayPal", Glyph: "🅿️", URL: "https://mcp.paypal.com/mcp", Description: "Invoices, orders, and transactions in PayPal.", Category: "Business"},
	{Name: "square", Display: "Square", Glyph: "⬛", URL: "https://mcp.squareup.com/sse", Description: "Payments and catalog in Square.", Category: "Business"},
	{Name: "plaid", Display: "Plaid", Glyph: "🏦", URL: "https://api.dashboard.plaid.com/mcp/sse", Description: "Financial accounts and transactions via Plaid.", Category: "Business"},
}

// Directory returns the curated connector catalog (a copy, so callers can't
// mutate the package list).
func Directory() []CatalogEntry {
	out := make([]CatalogEntry, len(catalog))
	copy(out, catalog)
	return out
}

// CatalogByName returns the curated entry for a connector name (ok=false when
// it's not a directory connector — e.g. a hand-added custom URL).
func CatalogByName(name string) (CatalogEntry, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, e := range catalog {
		if strings.ToLower(e.Name) == name {
			return e, true
		}
	}
	return CatalogEntry{}, false
}
