//! A reader for the handful of tiny XML documents GlobalProtect speaks.
//!
//! These responses are a flat `<jnlp>` of `<argument>` elements, a
//! `<challenge>` of two fields, or a one-line error. Pulling the text out of
//! named elements is the entire requirement, so that is all this does — a full
//! XML parser would be a dependency bought for nothing.
//!
//! Element names are matched case-insensitively: openconnect compares them
//! that way because real gateways send `inputStr` where the protocol notes say
//! `inputstr`.

/// Text of every element with this name, in document order.
///
/// A self-closing or empty element contributes an empty string rather than
/// being skipped: an `<argument/>` is a positional slot, and dropping it would
/// shift every argument after it onto the wrong meaning.
pub(super) fn values(xml: &str, tag: &str) -> Vec<String> {
    let mut found = Vec::new();
    let mut rest = xml;

    while let Some((body, remainder)) = next_element(rest, tag) {
        found.push(decode(body));
        rest = remainder;
    }
    found
}

/// Text of the first element with this name.
pub(super) fn value(xml: &str, tag: &str) -> Option<String> {
    next_element(xml, tag).map(|(body, _)| decode(body))
}

/// The whole of every `<tag …>…</tag>`, markup and attributes included.
///
/// AnyConnect needs this where GlobalProtect did not: its forms are described
/// by the *attributes* of `<input>` elements, and its `<opaque>` blob has to be
/// echoed back to the gateway byte for byte, children and all.
pub(super) fn raw_elements<'a>(xml: &'a str, tag: &str) -> Vec<&'a str> {
    let mut found = Vec::new();
    let mut rest = xml;

    while let Some((element, remainder)) = next_raw_element(rest, tag) {
        found.push(element);
        rest = remainder;
    }
    found
}

/// The whole of the first `<tag …>…</tag>`.
pub(super) fn raw_element<'a>(xml: &'a str, tag: &str) -> Option<&'a str> {
    next_raw_element(xml, tag).map(|(element, _)| element)
}

/// The value of an attribute on an element's opening tag.
///
/// Only the forms a gateway actually writes — `name="value"` and
/// `name='value'`. An attribute with no quotes is not XML.
pub(super) fn attribute(element: &str, name: &str) -> Option<String> {
    let open_end = element.find('>')?;
    let mut rest = element.get(..open_end)?;

    while let Some(at) = rest.find(name) {
        let before = rest[..at].chars().next_back();
        let after = rest.get(at + name.len()..)?;
        let after_trimmed = after.trim_start();
        // `name=` and not `some-other-name=`: the character before has to be
        // whitespace, and the next non-space one an equals sign.
        if before.is_none_or(char::is_whitespace) && after_trimmed.starts_with('=') {
            let value = after_trimmed.get(1..)?.trim_start();
            let quote = value.chars().next()?;
            if quote == '"' || quote == '\'' {
                let end = value.get(1..)?.find(quote)? + 1;
                return Some(decode(value.get(1..end)?));
            }
            return None;
        }
        rest = after;
    }
    None
}

/// Finds the next `<tag …>…</tag>`, returning the whole element and what
/// follows it.
fn next_raw_element<'a>(xml: &'a str, tag: &str) -> Option<(&'a str, &'a str)> {
    let start = element_start(xml, tag)?;
    let (_, rest) = next_element(xml.get(start..)?, tag)?;
    let consumed = xml.len() - rest.len();
    Some((xml.get(start..consumed)?, rest))
}

/// Offset of the `<` that opens the next element with this name.
fn element_start(xml: &str, tag: &str) -> Option<usize> {
    let mut cursor = 0;
    loop {
        let open = xml.get(cursor..)?.find('<')? + cursor;
        let after = open + 1;
        let name_end = xml
            .get(after..)?
            .find(|c: char| c == '>' || c == '/' || c.is_ascii_whitespace())
            .map(|offset| after + offset)?;
        if xml.get(after..name_end)?.eq_ignore_ascii_case(tag) {
            return Some(open);
        }
        cursor = after;
    }
}

/// Finds the next `<tag …>…</tag>`, returning its raw body and what follows.
fn next_element<'a>(xml: &'a str, tag: &str) -> Option<(&'a str, &'a str)> {
    let mut cursor = 0;

    loop {
        let open = xml.get(cursor..)?.find('<')? + cursor;
        let after = open + 1;
        let name_end = xml
            .get(after..)?
            .find(|c: char| c == '>' || c == '/' || c.is_ascii_whitespace())
            .map(|offset| after + offset)?;
        let name = xml.get(after..name_end)?;

        if !name.eq_ignore_ascii_case(tag) {
            cursor = after;
            continue;
        }

        let tag_end = xml.get(name_end..)?.find('>').map(|o| name_end + o)?;
        // `<argument/>` — an empty positional slot, not an absent one.
        if xml.get(..tag_end)?.ends_with('/') {
            return Some(("", xml.get(tag_end + 1..)?));
        }

        let body_start = tag_end + 1;
        let close = format!("</{name}>");
        let body_end = xml
            .get(body_start..)?
            .to_ascii_lowercase()
            .find(&close.to_ascii_lowercase())
            .map(|offset| body_start + offset)?;

        return Some((
            xml.get(body_start..body_end)?,
            xml.get(body_end + close.len()..)?,
        ));
    }
}

/// Resolves the XML entities a gateway actually emits. A 2FA prompt is free
/// text written by an administrator, so it is the one field that reliably
/// contains them.
fn decode(raw: &str) -> String {
    let raw = raw.trim();
    if !raw.contains('&') {
        return String::from(raw);
    }

    let mut out = String::with_capacity(raw.len());
    let mut rest = raw;
    while let Some(start) = rest.find('&') {
        out.push_str(&rest[..start]);
        let tail = &rest[start..];
        let Some(end) = tail.find(';').filter(|end| *end <= 10) else {
            out.push('&');
            rest = &tail[1..];
            continue;
        };
        let entity = &tail[1..end];
        match entity {
            "amp" => out.push('&'),
            "lt" => out.push('<'),
            "gt" => out.push('>'),
            "quot" => out.push('"'),
            "apos" => out.push('\''),
            _ => match numeric(entity) {
                Some(character) => out.push(character),
                None => {
                    out.push('&');
                    rest = &tail[1..];
                    continue;
                }
            },
        }
        rest = &tail[end + 1..];
    }
    out.push_str(rest);
    out
}

fn numeric(entity: &str) -> Option<char> {
    let digits = entity.strip_prefix('#')?;
    let code = match digits.strip_prefix(['x', 'X']) {
        Some(hex) => u32::from_str_radix(hex, 16).ok()?,
        None => digits.parse().ok()?,
    };
    char::from_u32(code)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn arguments_come_back_in_document_order() {
        let xml = "<jnlp><application-desc>\
            <argument>a</argument><argument>b</argument></application-desc></jnlp>";
        assert_eq!(values(xml, "argument"), vec!["a", "b"]);
    }

    #[test]
    fn an_empty_argument_keeps_its_slot() {
        // The arguments are positional: swallowing an empty one would shift
        // authcookie onto the meaning of the argument before it.
        let xml = "<argument></argument><argument>cookie</argument><argument/>";
        assert_eq!(values(xml, "argument"), vec!["", "cookie", ""]);
    }

    #[test]
    fn element_names_match_case_insensitively() {
        let xml = "<challenge><inputStr>ABC</inputStr></challenge>";
        assert_eq!(value(xml, "inputstr").as_deref(), Some("ABC"));
    }

    #[test]
    fn entities_in_a_prompt_are_resolved() {
        let xml = "<respmsg>Enter code &amp; press &lt;OK&gt; &#x2014; now</respmsg>";
        assert_eq!(
            value(xml, "respmsg").as_deref(),
            Some("Enter code & press <OK> — now")
        );
    }

    #[test]
    fn a_bare_ampersand_survives_rather_than_eating_the_rest() {
        assert_eq!(value("<msg>a & b</msg>", "msg").as_deref(), Some("a & b"));
    }

    #[test]
    fn a_missing_element_is_none() {
        assert_eq!(value("<jnlp></jnlp>", "challenge"), None);
        assert!(values("<jnlp></jnlp>", "argument").is_empty());
    }

    #[test]
    fn a_whole_element_comes_back_with_its_markup() {
        let xml = "<config-auth><opaque is-for=\"sg\"><tunnel-group>DefaultWEBVPNGroup\
                   </tunnel-group></opaque></config-auth>";
        // Echoed back to the gateway verbatim, so the children have to survive.
        assert_eq!(
            raw_element(xml, "opaque"),
            Some("<opaque is-for=\"sg\"><tunnel-group>DefaultWEBVPNGroup</tunnel-group></opaque>")
        );
        assert_eq!(raw_element(xml, "nothing"), None);
    }

    #[test]
    fn every_element_of_a_form_comes_back_in_order() {
        let xml = "<form><input type=\"text\" name=\"username\"/>\
                   <input type=\"password\" name=\"password\"/></form>";
        let inputs = raw_elements(xml, "input");
        assert_eq!(inputs.len(), 2);
        assert_eq!(attribute(inputs[0], "name").as_deref(), Some("username"));
        assert_eq!(attribute(inputs[1], "type").as_deref(), Some("password"));
    }

    #[test]
    fn attributes_are_read_off_the_opening_tag_only() {
        let element = "<input type=\'password\' name=\"secondary_password\" \
                       label=\"Answer &amp; continue:\">body name=\"lie\"</input>";
        assert_eq!(attribute(element, "type").as_deref(), Some("password"));
        assert_eq!(
            attribute(element, "label").as_deref(),
            Some("Answer & continue:")
        );
        // `name` must not match the tail of `secondary_password`, nor pick up
        // anything from the element body.
        assert_eq!(
            attribute(element, "name").as_deref(),
            Some("secondary_password")
        );
        assert_eq!(attribute(element, "missing"), None);
    }

    #[test]
    fn a_prefix_match_is_not_a_match() {
        // <arguments> must not be read as <argument>.
        assert!(values("<arguments>x</arguments>", "argument").is_empty());
    }
}
