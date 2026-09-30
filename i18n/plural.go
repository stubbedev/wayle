package i18n

// pluralRule maps plural operands to a CLDR cardinal category.
type pluralRule func(operands) string

// cardinalRules carries the intl_pluralrules 7.0.2 cardinal rules for
// the languages wayle ships translations in. fluent-bundle picks a
// bundle's rule set by lookup-negotiating the bundle locale against
// the rule table with "en" as the default; the coverage test asserts
// every embedded locale resolves to its own rule set here, so adding a
// locale without its rule fails loudly instead of borrowing English.
var cardinalRules = map[string]pluralRule{
	"en": func(o operands) string {
		if o.i == 1 && o.v == 0 {
			return "one"
		}
		return "other"
	},
	"fr": func(o operands) string {
		if o.i == 0 || o.i == 1 {
			return "one"
		}
		return "other"
	},
}

// ruleLocales lists the carried rule locales in the sorted order
// intl_pluralrules' table has.
var ruleLocales = []LangID{MustLangID("en"), MustLangID("fr")}

// pluralRuleFor ports fluent-bundle's PluralRules::construct.
func pluralRuleFor(locale LangID) (LangID, pluralRule) {
	chosen := negotiateLanguages([]LangID{locale}, ruleLocales, MustLangID("en"), lookup)[0]
	return chosen, cardinalRules[chosen.language]
}
