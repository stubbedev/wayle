//! `application/x-www-form-urlencoded` encoding.
//!
//! Both the login body and the session cookie are this format, and openconnect
//! URL-decodes the cookie it is handed — so a username containing `\` or a
//! password containing `&` has to arrive escaped or the pairs run together.

/// Encodes one value. Unreserved characters pass through; everything else
/// becomes `%XX`, uppercase, over the value's UTF-8 bytes.
///
/// Space is `%20`, not `+`: openconnect's decoder resolves percent escapes and
/// leaves `+` alone, so a `+` here would arrive as a literal plus sign.
fn escape(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(char::from(byte));
            }
            _ => out.push_str(&format!("%{byte:02X}")),
        }
    }
    out
}

/// Joins key/value pairs into a form body, escaping both sides.
pub(super) fn encode(pairs: &[(&str, &str)]) -> String {
    pairs
        .iter()
        .map(|(key, value)| format!("{}={}", escape(key), escape(value)))
        .collect::<Vec<_>>()
        .join("&")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unreserved_characters_pass_through_untouched() {
        assert_eq!(escape("aZ0-_.~"), "aZ0-_.~");
    }

    #[test]
    fn separators_are_escaped_so_pairs_cannot_run_together() {
        assert_eq!(escape("p@ss&word=x"), "p%40ss%26word%3Dx");
        assert_eq!(escape("EXAMPLE\\alice"), "EXAMPLE%5Calice");
    }

    #[test]
    fn a_space_is_percent_twenty_not_a_plus() {
        // openconnect resolves percent escapes but not `+`, so a plus would
        // arrive as a literal plus inside the password.
        assert_eq!(escape("a b"), "a%20b");
    }

    #[test]
    fn non_ascii_is_encoded_per_utf8_byte() {
        assert_eq!(escape("é"), "%C3%A9");
    }

    #[test]
    fn pairs_join_with_ampersands() {
        assert_eq!(encode(&[("a", "1"), ("b", "")]), "a=1&b=");
        assert_eq!(encode(&[]), "");
    }
}
