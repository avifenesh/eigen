package llm

import "testing"

func TestClassifyVisionWins(t *testing.T) {
	k, _ := Classify("look at this", true)
	if k != TaskVision {
		t.Fatal("an attached image should classify as vision")
	}
}

func TestClassifySearch(t *testing.T) {
	for _, p := range []string{"search the web for X", "what's the latest on Y", "look up the current price"} {
		if k, _ := Classify(p, false); k != TaskSearch {
			t.Errorf("%q should be search", p)
		}
	}
	if k, _ := Classify("write a function", false); k == TaskSearch {
		t.Error("plain coding should not be search")
	}
}

func TestClassifyDifficulty(t *testing.T) {
	if _, d := Classify("rename foo to bar", false); d != DiffTrivial {
		t.Error("rename should be trivial")
	}
	if _, d := Classify("debug this race condition", false); d != DiffHard {
		t.Error("debug/race should be hard")
	}
	if _, d := Classify("add a function to parse the config", false); d != DiffEasy {
		t.Error("short routine prompt should be easy")
	}
}

func TestParseTaskKindAndDifficulty(t *testing.T) {
	if k, ok := ParseTaskKind("search"); k != TaskSearch || !ok {
		t.Error("search parse")
	}
	if k, ok := ParseTaskKind(""); k != TaskGeneral || ok {
		t.Error("empty kind: general, not explicit")
	}
	if k, ok := ParseTaskKind("bogus"); k != TaskGeneral || ok {
		t.Error("bogus kind falls back to general, not explicit")
	}
	if d, ok := ParseDifficulty("hard"); d != DiffHard || !ok {
		t.Error("hard parse")
	}
	if d, ok := ParseDifficulty(""); d != DiffMedium || ok {
		t.Error("empty difficulty: medium, not explicit")
	}
}
