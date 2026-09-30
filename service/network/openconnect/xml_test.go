package openconnect

import (
	"slices"
	"testing"
)

func TestArgumentsComeBackInDocumentOrder(t *testing.T) {
	doc := "<jnlp><application-desc><argument>a</argument><argument>b</argument></application-desc></jnlp>"
	if got := xmlValues(doc, "argument"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("values = %q", got)
	}
}

func TestAnEmptyArgumentKeepsItsSlot(t *testing.T) {
	// The arguments are positional: swallowing an empty one would shift
	// authcookie onto the meaning of the argument before it.
	doc := "<argument></argument><argument>cookie</argument><argument/>"
	if got := xmlValues(doc, "argument"); !slices.Equal(got, []string{"", "cookie", ""}) {
		t.Errorf("values = %q", got)
	}
}

func TestElementNamesMatchCaseInsensitively(t *testing.T) {
	if got, ok := xmlValue("<challenge><inputStr>ABC</inputStr></challenge>", "inputstr"); !ok || got != "ABC" {
		t.Errorf("value = %q, %v", got, ok)
	}
}

func TestEntitiesInAPromptAreResolved(t *testing.T) {
	got, _ := xmlValue("<respmsg>Enter code &amp; press &lt;OK&gt; &#x2014; now</respmsg>", "respmsg")
	if got != "Enter code & press <OK> — now" {
		t.Errorf("value = %q", got)
	}
	// Decimal, and the forms that are not entities at all.
	if got := decodeEntities("&#65;&quot;&apos;&bogus;&#xD800;"); got != `A"'&bogus;&#xD800;` {
		t.Errorf("decoded = %q", got)
	}
}

func TestABareAmpersandSurvivesRatherThanEatingTheRest(t *testing.T) {
	if got, _ := xmlValue("<msg>a & b</msg>", "msg"); got != "a & b" {
		t.Errorf("value = %q", got)
	}
}

func TestAMissingElementIsAbsent(t *testing.T) {
	if _, ok := xmlValue("<jnlp></jnlp>", "challenge"); ok {
		t.Error("a missing element was found")
	}
	if got := xmlValues("<jnlp></jnlp>", "argument"); len(got) != 0 {
		t.Errorf("values = %q", got)
	}
}

func TestAWholeElementComesBackWithItsMarkup(t *testing.T) {
	doc := `<config-auth><opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup` +
		`</tunnel-group></opaque></config-auth>`
	// Echoed back to the gateway verbatim, so the children have to survive.
	got, ok := rawElement(doc, "opaque")
	if want := `<opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup</tunnel-group></opaque>`; !ok || got != want {
		t.Errorf("raw = %q, %v", got, ok)
	}
	if _, ok := rawElement(doc, "nothing"); ok {
		t.Error("a missing element was found")
	}
}

func TestEveryElementOfAFormComesBackInOrder(t *testing.T) {
	doc := `<form><input type="text" name="username"/><input type="password" name="password"/></form>`
	inputs := rawElements(doc, "input")
	if len(inputs) != 2 {
		t.Fatalf("inputs = %q", inputs)
	}
	if got, _ := xmlAttribute(inputs[0], "name"); got != "username" {
		t.Errorf("name = %q", got)
	}
	if got, _ := xmlAttribute(inputs[1], "type"); got != "password" {
		t.Errorf("type = %q", got)
	}
}

func TestAttributesAreReadOffTheOpeningTagOnly(t *testing.T) {
	element := `<input type='password' name="secondary_password" label="Answer &amp; continue:">body name="lie"</input>`
	if got, _ := xmlAttribute(element, "type"); got != "password" {
		t.Errorf("type = %q", got)
	}
	if got, _ := xmlAttribute(element, "label"); got != "Answer & continue:" {
		t.Errorf("label = %q", got)
	}
	// name must not match the tail of secondary_password, nor pick up
	// anything from the element body.
	if got, _ := xmlAttribute(element, "name"); got != "secondary_password" {
		t.Errorf("name = %q", got)
	}
	if _, ok := xmlAttribute(element, "missing"); ok {
		t.Error("a missing attribute was found")
	}
	// An unquoted value is not XML.
	if _, ok := xmlAttribute("<input name=bare>", "name"); ok {
		t.Error("an unquoted attribute was read")
	}
}

func TestAPrefixMatchIsNotAMatch(t *testing.T) {
	// <arguments> must not be read as <argument>.
	if got := xmlValues("<arguments>x</arguments>", "argument"); len(got) != 0 {
		t.Errorf("values = %q", got)
	}
}
