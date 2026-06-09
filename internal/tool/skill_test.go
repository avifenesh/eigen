package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeSkillSet struct{ bodies map[string]string }

func (f fakeSkillSet) Body(name string) (string, error) {
	if b, ok := f.bodies[name]; ok {
		return b, nil
	}
	return "", errNotFound
}
func (f fakeSkillSet) Names() []string { return []string{"alpha"} }

var errNotFound = errTest("not found")

type errTest string

func (e errTest) Error() string { return string(e) }

func runSkill(t *testing.T, set SkillSet, name string) (string, error) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"name": name})
	return Skill(set).Run(context.Background(), b)
}

func TestSkillLoadsBody(t *testing.T) {
	set := fakeSkillSet{bodies: map[string]string{"alpha": "# Alpha instructions"}}
	out, err := runSkill(t, set, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Alpha instructions") {
		t.Fatalf("skill body not returned: %q", out)
	}
}

func TestSkillUnknownErrors(t *testing.T) {
	set := fakeSkillSet{bodies: map[string]string{}}
	if _, err := runSkill(t, set, "missing"); err == nil {
		t.Fatal("unknown skill should error")
	}
}

func TestSkillRequiresName(t *testing.T) {
	set := fakeSkillSet{bodies: map[string]string{}}
	if _, err := Skill(set).Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing name should error")
	}
}

func TestSkillIsReadOnly(t *testing.T) {
	if !Skill(fakeSkillSet{}).ReadOnly {
		t.Fatal("skill tool should be read-only")
	}
}
