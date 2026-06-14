package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// Real syntax highlighting via chroma's per-language LEXERS, but mapped onto
// eigen's palette roles instead of a stock chroma theme — so highlighting is
// language-accurate AND on-brand (no clashing rainbow). This replaces the
// heuristic tintCodeLine for fenced/code-result blocks when a lexer is found;
// the heuristic remains the fallback for unknown languages.

// chromaRole maps a chroma token type to one of our code-token styles. The
// styles carry the given background so highlighted code sits on the surface.
func highlightCode(code, lang string, bg lipgloss.TerminalColor) (string, bool) {
	lexer := lexerFor(lang, code)
	if lexer == nil {
		return "", false
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return "", false
	}
	st := codeTokenStyles(bg)
	var b strings.Builder
	for _, tok := range it.Tokens() {
		b.WriteString(st.style(tok.Type).Render(tok.Value))
	}
	return b.String(), true
}

// lexerFor resolves a chroma lexer by language name, falling back to content
// analysis. Returns nil when nothing matches (caller uses the heuristic).
func lexerFor(lang, code string) chroma.Lexer {
	if lang != "" && lang != "code" {
		if l := lexers.Get(lang); l != nil {
			return l
		}
	}
	if l := lexers.Analyse(code); l != nil {
		return l
	}
	return nil
}

// codeStyles bundles the role styles for code tokens (all on one bg).
type codeStyles struct {
	keyword, str, num, comment, fn, typ, text, punct lipgloss.Style
}

func codeTokenStyles(bg lipgloss.TerminalColor) codeStyles {
	mk := func(c lipgloss.TerminalColor, bold, italic bool) lipgloss.Style {
		s := lipgloss.NewStyle().Foreground(c)
		if bg != nil {
			s = s.Background(bg)
		}
		if bold {
			s = s.Bold(true)
		}
		if italic {
			s = s.Italic(true)
		}
		return s
	}
	// Dedicated, DISTINCT syntax hues (not the cool/teal chrome roles, which
	// blur into one color) — a real editor palette tuned to the active theme.
	return codeStyles{
		keyword: mk(theme.SynKeyword, true, false),
		str:     mk(theme.SynString, false, false),
		num:     mk(theme.SynNumber, false, false),
		comment: mk(theme.SynComment, false, true),
		fn:      mk(theme.SynFunc, false, false),
		typ:     mk(theme.SynType, false, false),
		text:    mk(theme.Text, false, false),     // identifiers/variables — readable
		punct:   mk(theme.SynPunct, false, false), // operators/punctuation — soft
	}
}

// style picks a token style for a chroma token type, collapsing chroma's fine
// categories onto our small role set.
func (s codeStyles) style(t chroma.TokenType) lipgloss.Style {
	switch {
	case t.InCategory(chroma.Comment):
		return s.comment
	case t.InCategory(chroma.Keyword):
		switch t {
		case chroma.KeywordType:
			return s.typ
		default:
			return s.keyword
		}
	case t.InCategory(chroma.String):
		return s.str
	case t.InCategory(chroma.Number) || t == chroma.LiteralStringChar:
		return s.num
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return s.fn
	case t == chroma.NameClass || t == chroma.NameNamespace || t.InSubCategory(chroma.NameBuiltin) || t == chroma.KeywordType:
		return s.typ
	case t == chroma.NameConstant || t == chroma.KeywordConstant || t.InCategory(chroma.Literal):
		return s.num
	case t.InCategory(chroma.Operator) || t.InCategory(chroma.Punctuation):
		return s.punct
	case t.InCategory(chroma.Name):
		return s.text // identifiers / variables — readable, not dim
	default:
		return s.text
	}
}
