//! ScreenCast restore tokens.
//!
//! The portal `restore_data` is a `(suv)` variant — `(vendor, version, data)`.
//! The frontend stores it opaquely and hands it back in a later
//! `SelectSources` so the backend can replay the prior selection without
//! prompting. We stash the chosen [`CaptureTarget`] as a payload string in the
//! `data` field.

use zbus::zvariant::{OwnedValue, Value};

use super::source::CaptureTarget;

const VENDOR: &str = "wayle";
const VERSION: u32 = 1;

/// Encodes a target as portal `restore_data` (`(suv)`).
///
/// # Errors
///
/// Returns an error if the value cannot be made owned.
pub fn encode(target: &CaptureTarget) -> Result<OwnedValue, zbus::zvariant::Error> {
    let data = Value::new(target.to_payload());
    let structure = Value::from((VENDOR.to_string(), VERSION, data));
    OwnedValue::try_from(structure)
}

/// Decodes `restore_data` back into a target, returning `None` if it is not a
/// recognizable Wayle token.
#[must_use]
pub fn decode(value: &Value<'_>) -> Option<CaptureTarget> {
    let Value::Structure(structure) = value else {
        return None;
    };
    let fields = structure.fields();
    let [vendor, _version, data] = fields else {
        return None;
    };
    let Value::Str(vendor) = vendor else {
        return None;
    };
    if vendor.as_str() != VENDOR {
        return None;
    }
    let Value::Value(inner) = data else {
        return None;
    };
    let Value::Str(payload) = inner.as_ref() else {
        return None;
    };
    CaptureTarget::from_payload(payload.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn roundtrips_each_target() {
        for target in [
            CaptureTarget::Output("DP-1".to_owned()),
            CaptureTarget::Window("kitty@i-1".to_owned()),
            CaptureTarget::Region {
                output: "DP-2".to_owned(),
                x: 5,
                y: 6,
                width: 640,
                height: 480,
            },
        ] {
            let encoded = encode(&target).unwrap();
            let decoded = decode(&encoded).unwrap();
            assert_eq!(decoded, target);
        }
    }

    #[test]
    fn rejects_foreign_token() {
        let foreign = Value::from(("other-portal".to_string(), 1u32, Value::new("screen:DP-1")));
        assert_eq!(decode(&foreign), None);
    }

    #[test]
    fn rejects_non_structure() {
        assert_eq!(decode(&Value::from(42u32)), None);
    }
}
