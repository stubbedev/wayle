package launcher

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// This file ports the parts of nucleo-matcher 0.3.1 the launcher's
// matching runs on: the fzf pattern syntax (pattern.rs), the fuzzy,
// substring, prefix, postfix, and exact matchers (lib.rs, exact.rs,
// fuzzy_optimal.rs, fuzzy_greedy.rs, prefilter.rs), and the fzf-style
// scoring (score.rs) with nucleo's default config. One generic path
// over runes replaces nucleo's ASCII/Unicode specializations; the ASCII
// path's semantics are the ones kept.

// Scoring constants (score.rs), with Config::DEFAULT's boundaries.
const (
	scoreMatch               = 16
	penaltyGapStart          = 3
	penaltyGapExtension      = 1
	bonusBoundary            = scoreMatch / 2
	bonusCamel123            = bonusBoundary - penaltyGapStart
	bonusNonWord             = bonusBoundary
	bonusConsecutive         = penaltyGapStart + penaltyGapExtension
	bonusFirstCharMultiplier = 2
	bonusBoundaryWhite       = bonusBoundary + 2
	bonusBoundaryDelimiter   = bonusBoundary + 1
	// maxMatrixSize caps the optimal matcher's cells; past it the
	// greedy matcher scores instead (matrix.rs).
	maxMatrixSize = 100 * 1024
)

// charClass is chars.rs CharClass; the order matters (bonusFor).
type charClass uint8

const (
	classWhitespace charClass = iota
	classNonWord
	classDelimiter
	classLower
	classUpper
	classLetter
	classNumber
)

// delimiterChars is Config::DEFAULT's delimiter set.
const delimiterChars = "/,:;|"

// matchConfig is the per-atom half of nucleo's Config.
type matchConfig struct {
	ignoreCase bool
	normalize  bool
}

func classOf(c rune) charClass {
	if c < 0x80 {
		switch {
		case c >= 'a' && c <= 'z':
			return classLower
		case c >= 'A' && c <= 'Z':
			return classUpper
		case c >= '0' && c <= '9':
			return classNumber
		case c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r':
			return classWhitespace
		case strings.ContainsRune(delimiterChars, c):
			return classDelimiter
		default:
			return classNonWord
		}
	}
	switch {
	case unicode.IsLower(c):
		return classLower
	case isUpperCase(c):
		return classUpper
	case unicode.IsNumber(c):
		return classNumber
	case unicode.IsLetter(c):
		return classLetter
	case unicode.IsSpace(c):
		return classWhitespace
	default:
		return classNonWord
	}
}

// isUpperCase is chars::is_upper_case: the char has a simple case
// folding to something else.
func isUpperCase(c rune) bool { return unicode.ToLower(c) != c }

// normalizeRune is chars::normalize: latin variants of ASCII letters to
// the letter.
func normalizeRune(c rune) rune {
	if n, ok := normalizeTable[c]; ok {
		return n
	}
	return c
}

// norm is Char::normalize: what a haystack char compares as.
func (cfg matchConfig) norm(c rune) rune {
	if c < 0x80 {
		if cfg.ignoreCase && c >= 'A' && c <= 'Z' {
			return c + 32
		}
		return c
	}
	if cfg.normalize {
		c = normalizeRune(c)
	}
	if cfg.ignoreCase {
		c = unicode.ToLower(c)
	}
	return c
}

// classAndNorm is Char::char_class_and_normalize.
func (cfg matchConfig) classAndNorm(c rune) (rune, charClass) {
	class := classOf(c)
	if c < 0x80 {
		if cfg.ignoreCase && class == classUpper {
			c += 32
		}
		return c, class
	}
	fold := class == classUpper
	if cfg.normalize {
		c = normalizeRune(c)
		fold = true
	}
	if fold && cfg.ignoreCase {
		c = unicode.ToLower(c)
	}
	return c, class
}

// bonusFor is Config::bonus_for.
func bonusFor(prev, class charClass) int {
	if class > classDelimiter {
		switch prev {
		case classWhitespace:
			return bonusBoundaryWhite
		case classDelimiter:
			return bonusBoundaryDelimiter
		case classNonWord:
			return bonusBoundary
		}
	}
	switch {
	case prev == classLower && class == classUpper, prev != classNumber && class == classNumber:
		return bonusCamel123
	case class == classWhitespace:
		return bonusBoundaryWhite
	case class == classNonWord:
		return bonusNonWord
	}
	return 0
}

func satSub(a, b int) int { return max(a-b, 0) }

// graphemeRunes is Utf32Str::new: ASCII text rune for rune, anything
// else as the first char of each grapheme cluster.
func graphemeRunes(s string) []rune {
	ascii := true
	for i := range len(s) {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		out := make([]rune, len(s))
		for i := range len(s) {
			out[i] = rune(s[i])
		}
		return out
	}
	var out []rune
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		if rs := g.Runes(); len(rs) > 0 {
			out = append(out, rs[0])
		}
	}
	return out
}

// atomKind is pattern.rs AtomKind.
type atomKind uint8

const (
	atomFuzzy atomKind = iota
	atomSubstring
	atomPrefix
	atomPostfix
	atomExact
)

// atom is one pattern word, matched by one matcher function.
type atom struct {
	negative bool
	kind     atomKind
	needle   []rune
	cfg      matchConfig
}

// parsePattern is Pattern::parse: words split on unescaped spaces,
// each parsed with the fzf syntax; empty needles dropped.
func parsePattern(pattern string, ignoreCase, normalize bool) []atom {
	var atoms []atom
	for _, word := range patternWords(pattern) {
		if a := parseAtom(word, ignoreCase, normalize); len(a.needle) > 0 {
			atoms = append(atoms, a)
		}
	}
	return atoms
}

// patternWords splits on spaces not preceded by a backslash.
func patternWords(pattern string) []string {
	var words []string
	var cur strings.Builder
	sawBackslash := false
	for _, c := range pattern {
		if c == ' ' && !sawBackslash {
			words = append(words, cur.String())
			cur.Reset()
			continue
		}
		sawBackslash = c == '\\'
		cur.WriteRune(c)
	}
	return append(words, cur.String())
}

// parseAtom is Atom::parse: `!` negates, `^` prefix, `'` substring, a
// trailing `$` postfix (or exact after `^`), backslashes escape them.
// A negated fuzzy atom becomes a substring one.
func parseAtom(raw string, ignoreCase, normalize bool) atom {
	s := raw
	invert := false
	switch {
	case strings.HasPrefix(s, "!"):
		s, invert = s[1:], true
	case strings.HasPrefix(s, `\!`):
		s = s[1:]
	}
	kind := atomFuzzy
	switch {
	case strings.HasPrefix(s, "^"):
		s, kind = s[1:], atomPrefix
	case strings.HasPrefix(s, "'"):
		s, kind = s[1:], atomSubstring
	case strings.HasPrefix(s, `\^`), strings.HasPrefix(s, `\'`):
		s = s[1:]
	}
	appendDollar := false
	switch {
	case strings.HasSuffix(s, `\$`):
		s, appendDollar = s[:len(s)-2], true
	case strings.HasSuffix(s, "$"):
		if kind == atomFuzzy {
			kind = atomPostfix
		} else {
			kind = atomExact
		}
		s = s[:len(s)-1]
	}
	if invert && kind == atomFuzzy {
		kind = atomSubstring
	}
	a := newAtom(s, ignoreCase, normalize, kind, appendDollar)
	a.negative = invert
	return a
}

// newAtom is Atom::new_inner with whitespace escaping on.
func newAtom(needle string, ignoreCase, normalize bool, kind atomKind, appendDollar bool) atom {
	a := atom{kind: kind, cfg: matchConfig{ignoreCase: ignoreCase, normalize: normalize}}
	ascii := true
	for i := range len(needle) {
		if needle[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		needle = strings.ReplaceAll(needle, `\ `, " ")
		if ignoreCase {
			needle = strings.ToLower(needle)
		}
		if appendDollar {
			needle += "$"
		}
		a.needle = []rune(needle)
		return a
	}
	// nucleo's non-ASCII escape handling, verbatim: normalization is
	// only smart while no needle char would itself normalize.
	sawBackslash := false
	for _, c := range graphemeRunes(needle) {
		if sawBackslash {
			if c == ' ' {
				a.needle = append(a.needle, ' ')
				sawBackslash = false
				continue
			}
			a.needle = append(a.needle, '\\')
		}
		sawBackslash = c == '\\'
		if ignoreCase {
			c = unicode.ToLower(c)
		}
		a.cfg.normalize = a.cfg.normalize && normalizeRune(c) == c
		a.needle = append(a.needle, c)
	}
	if appendDollar {
		a.needle = append(a.needle, '$')
	}
	return a
}

// score is Atom::score: a negative atom scores 0 when it does not
// match and fails the item when it does.
func (a atom) score(hay []rune) (int, bool) {
	var s int
	var ok bool
	switch a.kind {
	case atomExact:
		s, ok = a.cfg.exactMatch(hay, a.needle)
	case atomFuzzy:
		s, ok = a.cfg.fuzzyMatch(hay, a.needle)
	case atomSubstring:
		s, ok = a.cfg.substringMatch(hay, a.needle)
	case atomPrefix:
		s, ok = a.cfg.prefixMatch(hay, a.needle)
	case atomPostfix:
		s, ok = a.cfg.postfixMatch(hay, a.needle)
	}
	if a.negative {
		return 0, !ok
	}
	return s, ok
}

// patternScore is Pattern::score: every atom must pass; scores add.
func patternScore(atoms []atom, hay []rune) (int, bool) {
	total := 0
	for _, a := range atoms {
		s, ok := a.score(hay)
		if !ok {
			return 0, false
		}
		total += s
	}
	return total, true
}

func isSpace(c rune) bool {
	if c < 0x80 {
		return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
	}
	return unicode.IsSpace(c)
}

func leadingSpace(hay []rune) int {
	n := 0
	for n < len(hay) && isSpace(hay[n]) {
		n++
	}
	return n
}

func trailingSpace(hay []rune) int {
	n := 0
	for n < len(hay) && isSpace(hay[len(hay)-1-n]) {
		n++
	}
	return n
}

// exactMatchImpl is Matcher::exact_match_impl: hay[start:end] must be
// the needle. (nucleo 0.3.1 compares the whole haystack on its ASCII
// case-sensitive path, which makes case-sensitive prefix and postfix
// atoms fail; the slice is what it means and what this compares.)
func (cfg matchConfig) exactMatchImpl(hay, needle []rune, start, end int) (int, bool) {
	if len(needle) != end-start {
		return 0, false
	}
	for i, n := range needle {
		if cfg.norm(hay[start+i]) != cfg.norm(n) {
			return 0, false
		}
	}
	return cfg.calculateScore(hay, needle, start, end), true
}

func (cfg matchConfig) exactMatch(hay, needle []rune) (int, bool) {
	if len(needle) == 0 {
		return 0, true
	}
	lead, trail := 0, 0
	if !isSpace(needle[0]) {
		lead = leadingSpace(hay)
	}
	if !isSpace(needle[len(needle)-1]) {
		trail = trailingSpace(hay)
	}
	if trail == len(hay) {
		return 0, false
	}
	return cfg.exactMatchImpl(hay, needle, lead, len(hay)-trail)
}

func (cfg matchConfig) prefixMatch(hay, needle []rune) (int, bool) {
	if len(needle) == 0 {
		return 0, true
	}
	lead := 0
	if !isSpace(needle[0]) {
		lead = leadingSpace(hay)
	}
	if len(hay)-lead < len(needle) {
		return 0, false
	}
	return cfg.exactMatchImpl(hay, needle, lead, lead+len(needle))
}

func (cfg matchConfig) postfixMatch(hay, needle []rune) (int, bool) {
	if len(needle) == 0 {
		return 0, true
	}
	trail := 0
	if !isSpace(needle[len(needle)-1]) {
		trail = trailingSpace(hay)
	}
	if len(hay)-trail < len(needle) {
		return 0, false
	}
	return cfg.exactMatchImpl(hay, needle, len(hay)-len(needle)-trail, len(hay)-trail)
}

// prevClass is the class before position i: the haystack's, or the
// config's initial class (whitespace) at the start.
func prevClass(hay []rune, i int) charClass {
	if i == 0 {
		return classWhitespace
	}
	return classOf(hay[i-1])
}

// substringMatch1 scores the best single-char occurrence
// (substring_match_1_ascii): the first occurrence with the highest
// first-char bonus, stopping at a whitespace boundary.
func (cfg matchConfig) substringMatch1(hay []rune, c rune) (int, bool) {
	best := 0
	for i, h := range hay {
		if cfg.norm(h) != c {
			continue
		}
		bonus := bonusFor(prevClass(hay, i), classOf(h))
		score := bonus*bonusFirstCharMultiplier + scoreMatch
		if score > best {
			best = score
			if bonus >= bonusBoundaryWhite {
				break
			}
		}
	}
	return best, best != 0
}

func (cfg matchConfig) substringMatch(hay, needle []rune) (int, bool) {
	switch {
	case len(needle) > len(hay):
		return 0, false
	case len(needle) == 0:
		return 0, true
	case len(needle) == len(hay):
		return cfg.exactMatchImpl(hay, needle, 0, len(hay))
	case len(needle) == 1:
		return cfg.substringMatch1(hay, needle[0])
	}
	best, bestPos := 0, 0
	for i := 0; i+len(needle) <= len(hay); i++ {
		if !cfg.equalAt(hay, needle, i) {
			continue
		}
		bonus := bonusFor(prevClass(hay, i), classOf(hay[i]))
		score := bonus*bonusFirstCharMultiplier + scoreMatch
		if score > best {
			best, bestPos = score, i
			if bonus >= bonusBoundaryWhite {
				break
			}
		}
	}
	if best == 0 {
		return 0, false
	}
	return cfg.calculateScore(hay, needle, bestPos, bestPos+len(needle)), true
}

func (cfg matchConfig) equalAt(hay, needle []rune, at int) bool {
	for j, n := range needle {
		if cfg.norm(hay[at+j]) != n {
			return false
		}
	}
	return true
}

// prefilter is prefilter_ascii: the first needle char's position, the
// end of the greedy forward match, and one past the last occurrence of
// the last needle char after it.
func (cfg matchConfig) prefilter(hay, needle []rune) (start, greedyEnd, end int, ok bool) {
	start = -1
	for i := range len(hay) - len(needle) + 1 {
		if cfg.norm(hay[i]) == needle[0] {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, 0, false
	}
	greedyEnd = start + 1
	for _, c := range needle[1:] {
		idx := -1
		for j := greedyEnd; j < len(hay); j++ {
			if cfg.norm(hay[j]) == c {
				idx = j
				break
			}
		}
		if idx < 0 {
			return 0, 0, 0, false
		}
		greedyEnd = idx + 1
	}
	end = greedyEnd
	last := needle[len(needle)-1]
	for j := len(hay) - 1; j >= greedyEnd; j-- {
		if cfg.norm(hay[j]) == last {
			end = j + 1
			break
		}
	}
	return start, greedyEnd, end, true
}

func (cfg matchConfig) fuzzyMatch(hay, needle []rune) (int, bool) {
	switch {
	case len(needle) > len(hay):
		return 0, false
	case len(needle) == 0:
		return 0, true
	case len(needle) == len(hay):
		return cfg.exactMatchImpl(hay, needle, 0, len(hay))
	case len(needle) == 1:
		return cfg.substringMatch1(hay, needle[0])
	}
	start, greedyEnd, end, ok := cfg.prefilter(hay, needle)
	if !ok {
		return 0, false
	}
	if len(needle) == end-start {
		return cfg.calculateScore(hay, needle, start, greedyEnd), true
	}
	if (end-start)*len(needle) > maxMatrixSize || end-start > 0xffff {
		return cfg.fuzzyGreedy(hay, needle, start, greedyEnd), true
	}
	return cfg.fuzzyOptimal(hay, needle, start, end)
}

// calculateScore is Matcher::calculate_score over a match known to
// start at start and end by end.
func (cfg matchConfig) calculateScore(hay, needle []rune, start, end int) int {
	prev := prevClass(hay, start)
	ni := 0
	needleChar := needle[0]
	inGap := false
	consecutive := 1
	class := classOf(hay[start])
	firstBonus := bonusFor(prev, class)
	score := scoreMatch + firstBonus*bonusFirstCharMultiplier
	prev = class
	if ni+1 < len(needle) {
		ni++
		needleChar = needle[ni]
	}
	for i := start + 1; i < end; i++ {
		c, class := cfg.classAndNorm(hay[i])
		if c == needleChar {
			bonus := bonusFor(prev, class)
			if consecutive != 0 {
				if bonus >= bonusBoundary && bonus > firstBonus {
					firstBonus = bonus
				}
				bonus = max(bonus, firstBonus, bonusConsecutive)
			} else {
				firstBonus = bonus
			}
			score += scoreMatch + bonus
			inGap = false
			consecutive++
			if ni+1 < len(needle) {
				ni++
				needleChar = needle[ni]
			}
		} else {
			penalty := penaltyGapStart
			if inGap {
				penalty = penaltyGapExtension
			}
			score = satSub(score, penalty)
			inGap = true
			consecutive = 0
		}
		prev = class
	}
	return score
}

// fuzzyGreedy is fuzzy_match_greedy_ on the ASCII path: shrink the
// greedy window from the right, then score it.
func (cfg matchConfig) fuzzyGreedy(hay, needle []rune, start, end int) int {
	ni := len(needle) - 1
	for i := end - 1; i >= start; i-- {
		if cfg.norm(hay[i]) != needle[ni] {
			continue
		}
		if ni == 0 {
			start = i
			break
		}
		ni--
	}
	return cfg.calculateScore(hay, needle, start, end)
}

// scoreCell is matrix.rs ScoreCell.
type scoreCell struct {
	score       int
	consecutive int
	matched     bool
}

// unmatched is fuzzy_optimal.rs UNMATCHED.
var unmatched = scoreCell{matched: true}

func nextMCell(p, bonus int, m scoreCell) scoreCell {
	if m == unmatched {
		return scoreCell{score: p + bonus + scoreMatch, consecutive: bonus}
	}
	consecutive := max(m.consecutive, bonusConsecutive)
	if bonus >= bonusBoundary && bonus > consecutive {
		consecutive = bonus
	}
	scoreMatchPath := m.score + max(consecutive, bonus)
	scoreSkip := p + bonus
	if scoreMatchPath > scoreSkip {
		return scoreCell{score: scoreMatchPath + scoreMatch, matched: true, consecutive: consecutive}
	}
	return scoreCell{score: scoreSkip + scoreMatch, consecutive: bonus}
}

func pScore(prevP, prevM int) int {
	return max(satSub(prevM, penaltyGapStart), satSub(prevP, penaltyGapExtension))
}

// fuzzyOptimal is fuzzy_match_optimal: the Smith-Waterman-style
// dynamic program over hay[start:end], each needle char's row starting
// at its greedy first position.
func (cfg matchConfig) fuzzyOptimal(hay, needle []rune, start, end int) (int, bool) {
	w := end - start
	hs := make([]rune, w)
	bonus := make([]int, w)
	prev := prevClass(hay, start)
	rowOffs := make([]int, len(needle))
	ni := 0
	matched := false
	for j := range w {
		c, class := cfg.classAndNorm(hay[start+j])
		hs[j] = c
		bonus[j] = bonusFor(prev, class)
		prev = class
		if c != needle[ni] {
			continue
		}
		if ni+1 < len(needle) {
			rowOffs[ni] = j
			ni++
		} else if !matched {
			rowOffs[ni] = j
			matched = true
		}
	}
	if !matched {
		return 0, false
	}
	row := make([]scoreCell, w)
	for j := rowOffs[0]; j < w; j++ {
		if hs[j] == needle[0] {
			row[j] = scoreCell{score: bonus[j]*bonusFirstCharMultiplier + scoreMatch, consecutive: bonus[j]}
		} else {
			row[j] = unmatched
		}
	}
	for i := 0; i+1 < len(needle); i++ {
		next := make([]scoreCell, w)
		prevP, prevM := 0, 0
		for j := rowOffs[i]; j < w; j++ {
			p := pScore(prevP, prevM)
			m := row[j]
			if j+1 < w && j+1 >= rowOffs[i+1] {
				if hs[j+1] == needle[i+1] {
					next[j+1] = nextMCell(p, bonus[j+1], m)
				} else {
					next[j+1] = unmatched
				}
			}
			prevP, prevM = p, m.score
		}
		row = next
	}
	best := 0
	for j := rowOffs[len(needle)-1]; j < w; j++ {
		best = max(best, row[j].score)
	}
	return best, true
}
