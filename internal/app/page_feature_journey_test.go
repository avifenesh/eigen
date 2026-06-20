package app

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	pluginpkg "github.com/avifenesh/eigen/internal/plugin"
	"github.com/avifenesh/eigen/internal/remote"
	"github.com/avifenesh/eigen/internal/skill"
)

func TestAppSessionsPageFeatureJourney(t *testing.T) {
	d := &Data{Config: config.Config{}, Skills: skill.Discover(), Sessions: []SessionRow{
		{ID: "s1", Title: "fix parser", Dir: "/p", Msgs: 3, Updated: 2},
		{ID: "s2", Title: "docs", Dir: "/p", Msgs: 1, Updated: 1},
	}}
	m := NewAt(d, PageSessions)
	m.width, m.height = 120, 30
	m.sessions.filter.searching = true
	for _, r := range "fix" {
		m.Update(key(string(r)))
	}
	m.sessions.filter.searching = false
	if len(m.sessions.visIdx) != 1 {
		t.Fatalf("session filter should narrow to one row, got %v", m.sessions.visIdx)
	}
	_, cmd := m.sessions.update(m, key("enter"))
	if cmd == nil || m.result.Action != ActionResume || m.result.SessionID != "s1" {
		t.Fatalf("enter should resume filtered session, result=%+v cmd=%v", m.result, cmd)
	}
}

func TestAppProjectsPageFeatureJourney(t *testing.T) {
	d := &Data{Config: config.Config{}, Skills: skill.Discover(), Projects: []ProjectRow{{
		Dir: "/repo", Name: "repo", Sessions: []SessionRow{{ID: "s1", Title: "repo task", Dir: "/repo", Updated: 1}},
	}}}
	m := NewAt(d, PageProjects)
	m.width, m.height = 120, 30
	_, cmd := m.projects.update(m, key("enter"))
	if cmd != nil || !m.projects.inside {
		t.Fatalf("enter on project should drill in without quitting, inside=%v cmd=%v", m.projects.inside, cmd)
	}
	_, cmd = m.projects.update(m, key("n"))
	if cmd == nil || m.result.Action != ActionOpenChat || m.result.Dir != "/repo" {
		t.Fatalf("n inside project should open chat rooted in project, result=%+v cmd=%v", m.result, cmd)
	}
}

func TestAppMachinesPageFeatureJourney(t *testing.T) {
	d := &Data{Config: config.Config{}, Skills: skill.Discover(), Machines: []remote.Machine{{Name: "dev", SSH: "ubuntu@x", Detected: true}}}
	m := NewAt(d, PageMachines)
	m.width, m.height = 120, 30
	_, cmd := m.machines.update(m, key("enter"))
	if cmd == nil || !m.machines.inside || !m.machines.loading {
		t.Fatalf("enter should drill into remote machine and fetch sessions, inside=%v loading=%v cmd=%v", m.machines.inside, m.machines.loading, cmd)
	}
	m.Update(machineSessionsMsg{mach: 0, sessions: []daemon.SessionInfo{{ID: "s1", Title: "remote task", Status: daemon.StatusIdle}}})
	_, cmd = m.machines.update(m, key("enter"))
	if cmd == nil || m.result.Action != ActionRemote || m.result.Host != "dev" || m.result.SessionID != "s1" {
		t.Fatalf("enter on remote session should attach remote, result=%+v cmd=%v", m.result, cmd)
	}
}

func TestAppConfigMemorySkillsFeatureJourneys(t *testing.T) {
	cfg := configModel(t)
	cursorTo(cfg, "perm")
	cfg.Update(key("space"))
	if cfg.data.Config.Perm != "auto" {
		t.Fatalf("config journey should cycle perm to auto, got %q", cfg.data.Config.Perm)
	}

	mem := memModel(t, "- first note\n- second note\n")
	mem.Update(key("enter"))
	if !mem.memory.open || !strings.Contains(mem.memory.view(mem, 80, 10), "first note") {
		t.Fatal("memory journey should open selected note detail")
	}

	sk := NewAt(&Data{Config: config.Config{}, Skills: skill.Discover()}, PageSkills)
	sk.width, sk.height = 120, 30
	view := sk.skills.view(sk, 90, 20)
	if !strings.Contains(view, "skills") {
		t.Fatalf("skills journey should render skill browser:\n%s", view)
	}
}

func TestAppProvidersModelsCronsFeatureJourneys(t *testing.T) {
	modelRows := Models()
	providerRows := Providers()
	if len(modelRows) == 0 || len(providerRows) == 0 {
		t.Fatal("catalog should expose providers and models")
	}
	d := &Data{Config: config.Config{}, Skills: skill.Discover()}
	m := NewAt(d, PageProviders)
	m.width, m.height = 120, 30
	if plain := m.View(); !strings.Contains(plain, providerRows[0].Name) {
		t.Fatalf("providers page should render provider catalog:\n%s", plain)
	}
	m.Update(key("down"))
	if len(m.providers.rows) > 1 && m.providers.list.cursor != 1 {
		t.Fatalf("providers page should support provider navigation, cursor=%d", m.providers.list.cursor)
	}
	m = NewAt(d, PageModels)
	m.width, m.height = 120, 30
	if plain := m.View(); !strings.Contains(plain, modelRows[0].ID) {
		t.Fatalf("models page should render model catalog:\n%s", plain)
	}
	m.Update(key("down"))
	if len(m.models.rows) > 1 && m.models.list.cursor != 1 {
		t.Fatalf("models page should support catalog navigation, cursor=%d", m.models.list.cursor)
	}
	m = NewAt(d, PageCrons)
	m.width, m.height = 120, 30
	m.crons.rows = []CronRow{{Name: "nightly", Kind: "timer", Next: "today 00:00", Command: "eigen dream", Active: true}}
	m.crons.loaded = true
	if plain := m.View(); !strings.Contains(plain, "nightly") || !strings.Contains(plain, "eigen dream") {
		t.Fatalf("crons page should render scheduled job details:\n%s", plain)
	}
}

func TestAppLivePageFeatureJourney(t *testing.T) {
	d := &Data{Config: config.Config{}, Skills: skill.Discover(), Live: []daemon.SessionInfo{{ID: "s1", Title: "live task", Dir: "/repo", Status: daemon.StatusIdle}}}
	m := NewAt(d, PageLive)
	m.width, m.height = 120, 30
	_, cmd := m.live.update(m, key("enter"))
	if cmd == nil || m.result.Action != ActionAttach || m.result.SessionID != "s1" || m.result.Dir != "/repo" {
		t.Fatalf("live enter should attach selected session, result=%+v cmd=%v", m.result, cmd)
	}
}

func TestAppPluginsPageFeatureJourney(t *testing.T) {
	seedPluginPage(t)
	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.plugins.load()
	if len(m.plugins.installed) != 1 || m.plugins.installed[0].Name != "demo" {
		t.Fatalf("plugins journey should load seeded installed plugin, got %+v", m.plugins.installed)
	}
	reg, _ := pluginpkg.NewRegistry()
	m.Update(key("enter"))
	m.plugins.reload()
	if pluginEnabled(m.plugins.installed[0], m.plugins.rows, reg) {
		t.Fatal("enter on installed plugin should disable it")
	}
	m.Update(key("2"))
	m.plugins.catalogMarket = "core"
	m.plugins.catalog = []pluginpkg.PluginEntry{{Name: "reviewer", Description: "Review code changes", Category: "coding"}}
	if v := m.plugins.view(m, 100, 30); !strings.Contains(v, "reviewer") || !strings.Contains(v, "Review code changes") {
		t.Fatalf("plugins marketplace journey should render catalog preview:\n%s", v)
	}
	m.Update(key("3"))
	if v := m.plugins.view(m, 100, 30); !strings.Contains(v, "wired components") || !strings.Contains(v, "demo-mcp") {
		t.Fatalf("plugins wiring journey should render wired components:\n%s", v)
	}
}

func TestAppHomePageResumeFeatureJourney(t *testing.T) {
	d := &Data{Config: config.Config{}, Skills: skill.Discover(), Sessions: []SessionRow{{ID: "s1", Source: "eigen", Title: "recent", Msgs: 1, Updated: 1}}}
	m := NewAt(d, PageHome)
	m.width, m.height = 120, 30
	m.home.syncFeed(d)
	m.home.list.cursor = m.home.feedN
	_, cmd := m.home.update(m, key("enter"))
	if cmd == nil || m.result.Action != ActionResume || m.result.SessionID != "s1" {
		t.Fatalf("home recent enter should resume session, result=%+v cmd=%v", m.result, cmd)
	}
}
