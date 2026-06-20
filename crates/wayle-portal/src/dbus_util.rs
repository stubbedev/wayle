//! Shared helpers for portal method `options`/`results` vardicts.
//!
//! Every interactive portal method takes an `a{sv}` options dict and returns an
//! `a{sv}` results dict. These helpers centralize the repetitive read/convert
//! and the owned-value wrapping so the interfaces stay terse and consistent.

use std::collections::HashMap;

use zbus::zvariant::{OwnedValue, Value};

/// A portal `a{sv}` dictionary.
pub type Vardict = HashMap<String, OwnedValue>;

/// Reads a `u32` option, accepting `u32`/`u64` encodings.
pub fn opt_u32(options: &Vardict, key: &str) -> Option<u32> {
    let value = options.get(key)?;
    u32::try_from(value)
        .ok()
        .or_else(|| u64::try_from(value).ok().and_then(|v| u32::try_from(v).ok()))
}

/// Reads a `bool` option.
pub fn opt_bool(options: &Vardict, key: &str) -> Option<bool> {
    bool::try_from(options.get(key)?).ok()
}

/// Reads a `String` option (clones out of the borrowed value).
pub fn opt_string(options: &Vardict, key: &str) -> Option<String> {
    String::try_from(options.get(key)?.try_clone().ok()?).ok()
}

/// Wraps a value as an [`OwnedValue`], discarding the (unreachable for these
/// fixed types) conversion error.
pub fn owned<'a>(value: impl Into<Value<'a>>) -> Option<OwnedValue> {
    OwnedValue::try_from(value.into()).ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dict(key: &str, value: Value<'static>) -> Vardict {
        let mut map = Vardict::new();
        map.insert(key.to_owned(), OwnedValue::try_from(value).unwrap());
        map
    }

    #[test]
    fn reads_u32_from_u32_and_u64() {
        assert_eq!(opt_u32(&dict("k", Value::from(7u32)), "k"), Some(7));
        assert_eq!(opt_u32(&dict("k", Value::from(7u64)), "k"), Some(7));
        assert_eq!(opt_u32(&dict("k", Value::from(7u32)), "missing"), None);
    }

    #[test]
    fn reads_bool_and_string() {
        assert_eq!(opt_bool(&dict("b", Value::from(true)), "b"), Some(true));
        assert_eq!(
            opt_string(&dict("s", Value::from("hi")), "s").as_deref(),
            Some("hi")
        );
    }

    #[test]
    fn owned_wraps_value() {
        assert!(owned(7u32).is_some());
        assert!(owned("x").is_some());
    }
}
