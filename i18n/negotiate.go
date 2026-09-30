package i18n

import (
	"slices"
	"strings"
)

// strategy is fluent-langneg's NegotiationStrategy.
type strategy int

const (
	filtering strategy = iota
	matching
	lookup
)

// control is what one comparison pass tells filterMatches to do next.
type control int

const (
	proceed     control = iota // keep trying looser comparisons
	nextRequest                // matching: this requested locale is served
	stopAll                    // lookup: one match is all we want
)

// filterMatches ports fluent-langneg 0.13 filter_matches: for every
// requested identifier it tries exact, available-as-range, maximized,
// variant-less, and region-less comparisons in turn, moving each
// matched available locale into the result.
func filterMatches(requested, available []LangID, mode strategy) []LangID {
	var supported []LangID
	remaining := slices.Clone(available)

	for _, want := range requested {
		req := want
		req.variants = slices.Clone(want.variants)
		test := func(selfRange, otherRange bool) control {
			found := false
			var kept []LangID
			for _, loc := range remaining {
				if mode != filtering && found {
					kept = append(kept, loc)
					continue
				}
				if loc.matches(req, selfRange, otherRange) {
					found = true
					supported = append(supported, loc)
					continue
				}
				kept = append(kept, loc)
			}
			remaining = kept
			switch {
			case !found || mode == filtering:
				return proceed
			case mode == matching:
				return nextRequest
			default:
				return stopAll
			}
		}
		outcome := func() control {
			if c := test(false, false); c != proceed {
				return c
			}
			if c := test(true, false); c != proceed {
				return c
			}
			if req.language == "" {
				return nextRequest
			}
			if req.maximize() {
				if c := test(true, false); c != proceed {
					return c
				}
			}
			req.variants = nil
			if c := test(true, true); c != proceed {
				return c
			}
			req.region = ""
			if req.maximize() {
				if c := test(true, false); c != proceed {
					return c
				}
			}
			req.region = ""
			return test(true, true)
		}()
		if outcome == stopAll {
			break
		}
	}
	return supported
}

// negotiateLanguages ports negotiate_languages: the filtered matches,
// with the default appended when absent (or, for lookup, used only
// when nothing matched).
func negotiateLanguages(requested, available []LangID, def LangID, mode strategy) []LangID {
	supported := filterMatches(requested, available, mode)
	if mode == lookup {
		if len(supported) == 0 {
			supported = append(supported, def)
		}
		return supported
	}
	if !slices.ContainsFunc(supported, def.Equal) {
		supported = append(supported, def)
	}
	return supported
}

// requestedLanguages ports i18n-embed's DesktopLanguageRequester on
// unix: sys-locale 0.3's list (every LANGUAGE entry, then LC_ALL,
// LC_MESSAGES, and LANG, each deduplicated as a string, with the
// encoding and modifier cut and '_' turned into '-'), dropping any tag
// unic-langid rejects ("C", "POSIX", empty entries).
func requestedLanguages(getenv func(string) string) []LangID {
	var tags []string
	add := func(raw string) {
		tag := posixToBCP47(raw)
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if v := getenv("LANGUAGE"); v != "" {
		for part := range strings.SplitSeq(v, ":") {
			add(part)
		}
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(name); v != "" {
			add(v)
		}
	}
	var ids []LangID
	for _, tag := range tags {
		if id, err := ParseLangID(tag); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// posixToBCP47 is sys-locale's posix_to_bcp47.
func posixToBCP47(locale string) string {
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	return strings.ReplaceAll(locale, "_", "-")
}
