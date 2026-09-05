//! WireGuard key generation.
//!
//! A WireGuard profile needs a private key the user is otherwise expected to
//! have produced with `wg genkey` elsewhere, and the server admin needs the
//! matching public key. Without this the form asks for a base64 blob it gives
//! no way to obtain, and the public key — which is derived, not stored — has
//! nowhere to be read off at all.
//!
//! Keys are X25519 in WireGuard's own encoding: 32 raw bytes, base64 standard
//! alphabet with padding, which is what `wg` prints and what NetworkManager
//! stores.

use base64::{Engine as _, engine::general_purpose::STANDARD};
use rand::RngCore;
use x25519_dalek::{PublicKey, StaticSecret};

/// A freshly generated private key and the public key derived from it, both
/// base64 as WireGuard writes them.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct KeyPair {
    /// The private key, for this end's `private-key`.
    pub private: String,
    /// The public key, to hand to whoever runs the other end.
    pub public: String,
}

/// Generates a new WireGuard key pair from the OS random source.
#[must_use]
pub fn generate() -> KeyPair {
    // `rand::rng()` rather than `OsRng`: it is a `CryptoRng` seeded from the
    // OS and it cannot fail, where `OsRng` is fallible in rand 0.9 and would
    // put a panic or an error path on a call that has nothing useful to say
    // when the OS random source is gone. x25519-dalek's own
    // `random_from_rng` is not usable here either — it is on rand_core 0.6
    // and the workspace is on rand 0.9, so their RNG traits do not meet.
    let mut bytes = [0_u8; 32];
    rand::rng().fill_bytes(&mut bytes);

    let secret = StaticSecret::from(bytes);
    let public = PublicKey::from(&secret);

    KeyPair {
        private: STANDARD.encode(secret.to_bytes()),
        public: STANDARD.encode(public.as_bytes()),
    }
}

/// Derives the public key for a base64 private key.
///
/// Returns `None` when the text is not a 32-byte base64 key, so a half-typed
/// or pasted-wrong private key shows nothing rather than a public key that
/// belongs to something else.
#[must_use]
pub fn public_for(private: &str) -> Option<String> {
    let bytes: [u8; 32] = STANDARD.decode(private.trim()).ok()?.try_into().ok()?;
    let public = PublicKey::from(&StaticSecret::from(bytes));

    Some(STANDARD.encode(public.as_bytes()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generated_keys_are_wireguard_shaped() {
        let pair = generate();

        // `wg` prints 32 bytes as 44 base64 characters, and NM stores exactly
        // that; anything else is rejected by the kernel module.
        assert_eq!(pair.private.len(), 44);
        assert_eq!(pair.public.len(), 44);
        assert_eq!(STANDARD.decode(&pair.private).unwrap().len(), 32);
        assert_eq!(STANDARD.decode(&pair.public).unwrap().len(), 32);
    }

    #[test]
    fn public_key_is_derived_from_the_private_one() {
        let pair = generate();

        assert_eq!(public_for(&pair.private).as_ref(), Some(&pair.public));
        // Surrounding whitespace is what a paste out of a terminal carries.
        assert_eq!(
            public_for(&format!("  {}\n", pair.private)).as_ref(),
            Some(&pair.public)
        );
    }

    #[test]
    fn each_generation_is_a_new_key() {
        assert_ne!(generate().private, generate().private);
    }

    #[test]
    fn a_key_that_is_not_a_key_derives_nothing() {
        // Not base64 at all, valid base64 of the wrong length, and empty: all
        // have to yield nothing rather than a plausible-looking public key.
        assert_eq!(public_for("not base64 !!"), None);
        assert_eq!(public_for(&STANDARD.encode([0_u8; 16])), None);
        assert_eq!(public_for(""), None);
    }

    #[test]
    fn a_known_vector_matches_wg_pubkey() {
        // Checked against real `wg pubkey` (wireguard-tools 1.0.20260223),
        // so this pins the encoding and the clamping rather than just
        // agreeing with ourselves.
        assert_eq!(
            public_for("yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=").as_deref(),
            Some("HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=")
        );
    }
}
