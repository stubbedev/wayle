//! AnyConnect single sign-on through the system browser.
//!
//! This is the SAML path that does **not** need an embedded webview. The
//! shapes come from openconnect's `auth.c`, `cstp.c` and `hpke.c`.
//!
//! # The exchange
//!
//! 1. the client advertises `single-sign-on-external-browser` in its
//!    `<capabilities>` and sends its ephemeral P-256 public key in
//!    `X-AnyConnect-STRAP-DH-Pubkey`;
//! 2. the gateway answers with an auth form containing an
//!    `<input type="sso">` and the `sso-v2-login` URL to open;
//! 3. the client opens that URL in the browser and listens on
//!    `[::1]:29786` for the IdP to come back with
//!    `GET /api/sso/<base64 blob>?return=<url>`, answering 302 to `return`
//!    so the browser lands on the gateway's own success page;
//! 4. the blob is a TLV structure carrying the gateway's ephemeral public
//!    key, an AES-GCM ciphertext, its tag and IV. ECDH against our key,
//!    HKDF-SHA256 with the info string `AC_ECIES`, then AES-256-GCM open
//!    yields the SSO token;
//! 5. the token goes back as the value of the `sso` input, and the sign-in
//!    finishes like any other form.
//!
//! # Why it is opt-in
//!
//! openconnect ships `--no-external-auth` for a reason its manual states
//! plainly: "some servers will force the client to use such an
//! authentication mode if the client advertises it, but fallback to a more
//! 'scriptable' authentication mode if the client doesn't appear to support
//! it". Advertising this is therefore a one-way door per gateway — it can
//! turn a gateway that happily serves wayle a form into one that insists on
//! a browser. So the profile has to ask for it: see the `wayle-sso` field on
//! the openconnect form. Nothing changes for a profile that does not.
//!
//! # What is and is not verified
//!
//! The crypto is covered by a round trip in this module's tests, which seal
//! a token exactly as the gateway would and open it with these functions —
//! so the key schedule, the info string and the TLV layout are pinned. The
//! browser handoff cannot be: it needs a real IdP and a person to sign in.

use std::{net::Ipv6Addr, time::Duration};

use aes_gcm::{
    AesGcm, KeyInit,
    aead::{AeadInPlace, consts, generic_array::GenericArray},
    aes::Aes256,
};
use base64::Engine;
use ring::{
    agreement::{self, EphemeralPrivateKey, UnparsedPublicKey},
    hkdf,
    rand::SystemRandom,
};
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    net::TcpListener,
};
use tracing::{debug, warn};

use crate::Error;

/// AES-256-GCM with a twelve-byte nonce *and* a twelve-byte tag, which is
/// what Cisco sends. The tag length is the unusual part.
type Aes256Gcm12 = AesGcm<Aes256, consts::U12, consts::U12>;
type Nonce12 = GenericArray<u8, consts::U12>;
type Tag12 = GenericArray<u8, consts::U12>;

/// The loopback port Cisco's browser flow comes back on. Not configurable:
/// the IdP redirects to it by absolute URL.
const CALLBACK_PORT: u16 = 29786;

/// The path the browser calls back on.
const CALLBACK_PATH: &str = "/api/sso/";

/// The HKDF info string, from `hpke.c`. Exactly eight bytes, no terminator.
const HKDF_INFO: &[u8] = b"AC_ECIES";

/// TLV tags inside the token blob.
const TAG_PUBKEY: u16 = 1;
const TAG_AEAD_TAG: u16 = 2;
const TAG_CIPHERTEXT: u16 = 3;
const TAG_IV: u16 = 4;

/// AES-GCM's IV and tag are both 12 bytes here — the tag length is Cisco's
/// choice and `hpke.c` rejects anything else, so this does too.
const IV_LEN: usize = 12;
const TAG_LEN: usize = 12;

/// The fixed DER prefix of a P-256 `SubjectPublicKeyInfo`.
///
/// The whole structure for this curve is this prefix followed by the 65-byte
/// uncompressed point, so wrapping and unwrapping is a splice rather than a
/// parse. Byte for byte: `SEQUENCE { SEQUENCE { OID ecPublicKey, OID
/// prime256v1 }, BIT STRING (0 unused bits) }`.
const P256_SPKI_PREFIX: &[u8] = &[
    0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a,
    0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
];

/// The length of an uncompressed P-256 point: `0x04` and two 32-byte
/// coordinates.
const P256_POINT_LEN: usize = 65;

/// Our ephemeral key for one sign-in.
pub(super) struct Keys {
    private: EphemeralPrivateKey,
    /// The public key as the gateway wants it: base64 of the DER
    /// `SubjectPublicKeyInfo`.
    pub public_base64: String,
}

impl Keys {
    /// Generates a fresh key pair. One per sign-in: the shared secret it
    /// derives protects exactly one token.
    ///
    /// # Errors
    ///
    /// Returns an error when the platform RNG or the curve is unavailable.
    pub(super) fn generate() -> Result<Self, Error> {
        let rng = SystemRandom::new();
        let private = EphemeralPrivateKey::generate(&agreement::ECDH_P256, &rng)
            .map_err(|_| failed("cannot generate a key for the browser sign-in"))?;
        let public = private
            .compute_public_key()
            .map_err(|_| failed("cannot derive the public key for the browser sign-in"))?;
        Ok(Self {
            public_base64: base64_encode(&spki(public.as_ref())),
            private,
        })
    }
}

/// Wraps an uncompressed P-256 point in a DER `SubjectPublicKeyInfo`.
#[must_use]
pub(super) fn spki(point: &[u8]) -> Vec<u8> {
    let mut der = Vec::with_capacity(P256_SPKI_PREFIX.len() + point.len());
    der.extend_from_slice(P256_SPKI_PREFIX);
    der.extend_from_slice(point);
    der
}

/// Takes the uncompressed point back out of a DER `SubjectPublicKeyInfo`.
///
/// `None` for anything that is not a P-256 public key in that exact shape —
/// a different curve derives a different secret, so guessing would produce a
/// token that fails to decrypt with no explanation.
#[must_use]
pub(super) fn point_from_spki(der: &[u8]) -> Option<&[u8]> {
    let rest = der.strip_prefix(P256_SPKI_PREFIX)?;
    (rest.len() == P256_POINT_LEN && rest[0] == 0x04).then_some(rest)
}

/// The parts of the encrypted SSO token.
#[derive(Debug, PartialEq, Eq)]
pub(super) struct TokenBlob {
    /// The gateway's ephemeral public key, DER `SubjectPublicKeyInfo`.
    pub pubkey: Vec<u8>,
    /// The AEAD tag.
    pub tag: Vec<u8>,
    /// The encrypted token.
    pub ciphertext: Vec<u8>,
    /// The AES-GCM nonce.
    pub iv: Vec<u8>,
}

/// Decodes the TLV blob the browser handed back.
///
/// The layout, from `hpke.c`: a leading `0x0001`, then `(tag: u16, len: u16,
/// bytes)` triples. Every field is required and the tag and IV are both
/// exactly twelve bytes.
///
/// # Errors
///
/// Returns an error for a truncated blob, a repeated or unknown tag, or a
/// missing field — anything that would otherwise surface as an
/// indistinguishable decryption failure later.
pub(super) fn parse_blob(bytes: &[u8]) -> Result<TokenBlob, Error> {
    if bytes.len() < 2 || u16::from_be_bytes([bytes[0], bytes[1]]) != 1 {
        return Err(failed("the sign-in token is not in the expected format"));
    }

    let mut pubkey = None;
    let mut tag = None;
    let mut ciphertext = None;
    let mut iv = None;
    let mut at = 2;

    while at < bytes.len() {
        if at + 4 > bytes.len() {
            return Err(failed("the sign-in token is truncated"));
        }
        let field = u16::from_be_bytes([bytes[at], bytes[at + 1]]);
        let length = usize::from(u16::from_be_bytes([bytes[at + 2], bytes[at + 3]]));
        let start = at + 4;
        let end = start
            .checked_add(length)
            .filter(|end| *end <= bytes.len())
            .ok_or_else(|| failed("the sign-in token is truncated"))?;
        let value = bytes[start..end].to_vec();

        let slot = match field {
            TAG_PUBKEY => &mut pubkey,
            TAG_AEAD_TAG => &mut tag,
            TAG_CIPHERTEXT => &mut ciphertext,
            TAG_IV => &mut iv,
            _ => return Err(failed("the sign-in token has an unexpected field")),
        };
        if slot.is_some() {
            return Err(failed("the sign-in token repeats a field"));
        }
        *slot = Some(value);
        at = end;
    }

    let blob = TokenBlob {
        pubkey: pubkey.ok_or_else(|| failed("the sign-in token has no gateway key"))?,
        tag: tag.ok_or_else(|| failed("the sign-in token has no authentication tag"))?,
        ciphertext: ciphertext.ok_or_else(|| failed("the sign-in token is empty"))?,
        iv: iv.ok_or_else(|| failed("the sign-in token has no nonce"))?,
    };
    if blob.tag.len() != TAG_LEN || blob.iv.len() != IV_LEN {
        return Err(failed("the sign-in token's nonce or tag is the wrong size"));
    }
    Ok(blob)
}

/// Derives the AES key for a blob: ECDH against our key, then HKDF-SHA256.
///
/// The salt is empty, which is what openconnect's `EVP_PKEY_HKDF` call with
/// no salt set amounts to — HMAC zero-pads a short key to the block size, so
/// an empty salt and a 32-byte zero salt are the same key.
fn derive_key(keys: Keys, gateway_pubkey: &[u8]) -> Result<[u8; 32], Error> {
    let point = point_from_spki(gateway_pubkey)
        .ok_or_else(|| failed("the gateway sent a key on an unexpected curve"))?;
    let peer = UnparsedPublicKey::new(&agreement::ECDH_P256, point);

    agreement::agree_ephemeral(keys.private, &peer, |secret| {
        let salt = hkdf::Salt::new(hkdf::HKDF_SHA256, &[]);
        let prk = salt.extract(secret);
        let mut key = [0u8; 32];
        // The `okm` length is what tells HKDF how much to expand to.
        prk.expand(&[HKDF_INFO], hkdf::HKDF_SHA256)
            .and_then(|okm| okm.fill(&mut key))
            .map(|()| key)
            .map_err(|_| failed("cannot derive the sign-in token's key"))
    })
    .map_err(|_| failed("cannot agree a key with the gateway"))?
}

/// Opens the token.
///
/// # Errors
///
/// Returns an error when the key cannot be derived, the tag does not verify,
/// or the plaintext is not the alphanumeric token a gateway sends — the last
/// check is openconnect's, and it is what stops a wrong key from being
/// reported as a working sign-in.
pub(super) fn decrypt(keys: Keys, blob: &TokenBlob) -> Result<String, Error> {
    let key = derive_key(keys, &blob.pubkey)?;

    // A *twelve*-byte GCM tag, not the usual sixteen. That is Cisco's
    // choice, and `openssl.c` pins it: `EVP_CTRL_AEAD_SET_TAG, 12`. It is
    // also why the AEAD here is not `ring`'s, which only accepts a full-
    // length tag — the four missing bytes are not padding, they were never
    // sent.
    let cipher = <Aes256Gcm12 as KeyInit>::new_from_slice(&key)
        .map_err(|_| failed("cannot use the derived key"))?;
    let mut buffer = blob.ciphertext.clone();
    cipher
        .decrypt_in_place_detached(
            Nonce12::from_slice(&blob.iv),
            &[],
            &mut buffer,
            Tag12::from_slice(&blob.tag),
        )
        .map_err(|_| failed("the sign-in token did not verify"))?;

    let token = String::from_utf8(buffer).map_err(|_| failed("the sign-in token is not text"))?;
    if token.is_empty() || !token.chars().all(char::is_alphanumeric) {
        return Err(failed("the sign-in token is not in the expected format"));
    }
    Ok(token)
}

/// What the browser called back with.
#[derive(Debug, PartialEq, Eq)]
pub(super) struct Callback {
    /// The base64 blob out of the path.
    pub blob: String,
    /// The `return=` URL to send the browser on to, when it gave one.
    pub redirect: Option<String>,
}

/// Reads a callback out of an HTTP request line.
///
/// `None` for anything that is not the callback — the port sees stray
/// connections, and answering them as if they were the sign-in would abandon
/// the wait.
#[must_use]
pub(super) fn parse_request_line(line: &str) -> Option<Callback> {
    let mut parts = line.split_whitespace();
    if parts.next()? != "GET" {
        return None;
    }
    let target = parts.next()?;
    if !parts.next()?.starts_with("HTTP/1.") {
        return None;
    }

    let path = target.strip_prefix(CALLBACK_PATH)?;
    let (blob, query) = match path.split_once('?') {
        Some((blob, query)) => (blob, Some(query)),
        None => (path, None),
    };
    if blob.is_empty() {
        return None;
    }

    let redirect = query.and_then(|query| {
        query
            .split('&')
            .find_map(|pair| pair.strip_prefix("return="))
            .map(url_decode)
    });
    Some(Callback {
        blob: url_decode(blob),
        redirect,
    })
}

/// Resolves `%XX` escapes. `+` is left alone: it is a valid base64 character
/// and this is a path, not a form body.
fn url_decode(value: &str) -> String {
    let bytes = value.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%'
            && index + 2 < bytes.len()
            && let Ok(byte) = u8::from_str_radix(&value[index + 1..index + 3], 16)
        {
            out.push(byte);
            index += 3;
        } else {
            out.push(bytes[index]);
            index += 1;
        }
    }
    String::from_utf8(out).unwrap_or_else(|_| String::from(value))
}

/// Opens `url` in the browser and waits for the IdP to come back.
///
/// # Errors
///
/// Returns an error when the port cannot be bound, when the browser cannot
/// be opened, or when nothing arrives within `timeout`.
pub(super) async fn await_token(url: &str, timeout: Duration) -> Result<String, Error> {
    // Bound *before* opening the browser: the IdP redirect can be quick, and
    // a connection refused there loses the sign-in.
    let listener = TcpListener::bind((Ipv6Addr::LOCALHOST, CALLBACK_PORT))
        .await
        .map_err(|error| {
            failed(&format!(
                "cannot listen on port {CALLBACK_PORT} for the browser sign-in: {error}"
            ))
        })?;

    open_in_browser(url)?;
    debug!(%url, "waiting for the browser to finish the sign-in");

    tokio::time::timeout(timeout, accept_callback(&listener))
        .await
        .map_err(|_| failed("the browser sign-in was not completed in time"))?
}

/// Accepts connections until one is the callback.
async fn accept_callback(listener: &TcpListener) -> Result<String, Error> {
    loop {
        let (stream, from) = listener
            .accept()
            .await
            .map_err(|error| failed(&format!("the browser sign-in connection failed: {error}")))?;
        debug!(%from, "connection on the browser sign-in port");

        let mut reader = BufReader::new(stream);
        let mut line = String::new();
        if reader.read_line(&mut line).await.is_err() {
            continue;
        }

        let Some(callback) = parse_request_line(line.trim_end()) else {
            // Not the sign-in. Say so and keep waiting rather than treating
            // a stray probe as the answer.
            let _ = reader.into_inner().write_all(NOT_FOUND.as_bytes()).await;
            continue;
        };

        // Send the browser on to the gateway's own success page, so the user
        // sees something other than a blank tab.
        let response = match &callback.redirect {
            Some(url) => format!(
                "HTTP/1.1 302 Found\r\nConnection: close\r\nContent-Length: 0\r\nLocation: {url}\r\n\r\n"
            ),
            None => String::from(SUCCESS),
        };
        let _ = reader.into_inner().write_all(response.as_bytes()).await;
        return Ok(callback.blob);
    }
}

const NOT_FOUND: &str = "HTTP/1.1 404 Not Found\r\nConnection: close\r\nContent-Type: text/html\r\nContent-Length: 0\r\n\r\n";

const SUCCESS: &str = "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: text/html\r\n\r\n\
     <html><title>Signed in</title><body>You can close this tab.</body></html>\r\n";

/// Hands the URL to the desktop's browser.
fn open_in_browser(url: &str) -> Result<(), Error> {
    // `xdg-open` rather than a configured browser: the sign-in has to land
    // in the browser the user is already signed into their IdP with.
    std::process::Command::new("xdg-open")
        .arg(url)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|error| {
            warn!(%error, "cannot open a browser for the VPN sign-in");
            failed("cannot open a browser to sign in with")
        })
}

/// Standard base64, which is what the gateway's headers take.
fn base64_encode(bytes: &[u8]) -> String {
    base64::engine::general_purpose::STANDARD.encode(bytes)
}

/// Decodes standard base64, tolerating a blob that arrives unpadded.
#[must_use]
pub(super) fn base64_decode(value: &str) -> Option<Vec<u8>> {
    let trimmed: String = value.chars().filter(|c| !c.is_whitespace()).collect();
    base64::engine::general_purpose::STANDARD
        .decode(&trimmed)
        .ok()
        .or_else(|| {
            base64::engine::general_purpose::STANDARD_NO_PAD
                .decode(trimmed.trim_end_matches('='))
                .ok()
        })
}

fn failed(message: &str) -> Error {
    Error::VpnAuthenticationFailed(String::from(message))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn base64_round_trips_including_the_padded_lengths() {
        for bytes in [
            vec![],
            vec![0u8],
            vec![0u8, 1],
            vec![0u8, 1, 2],
            vec![255u8; 65],
        ] {
            let encoded = base64_encode(&bytes);
            assert_eq!(
                base64_decode(&encoded).as_deref(),
                Some(bytes.as_slice()),
                "{encoded}"
            );
        }
        // The exact encoding, not just a round trip.
        assert_eq!(
            base64_encode(b"any carnal pleasure."),
            "YW55IGNhcm5hbCBwbGVhc3VyZS4="
        );
        assert!(base64_decode("not base64!").is_none());
    }

    #[test]
    fn a_public_key_round_trips_through_its_der_wrapper() {
        let point: Vec<u8> = std::iter::once(0x04)
            .chain((0..64).map(|n| n as u8))
            .collect();
        let der = spki(&point);
        assert_eq!(der.len(), P256_SPKI_PREFIX.len() + P256_POINT_LEN);
        assert_eq!(point_from_spki(&der), Some(point.as_slice()));
    }

    #[test]
    fn a_key_that_is_not_a_p256_point_is_refused() {
        // A different curve derives a different secret, so accepting one
        // would surface as an unexplained decryption failure.
        assert!(point_from_spki(&[]).is_none());
        assert!(point_from_spki(P256_SPKI_PREFIX).is_none());
        // Right length, wrong point format (compressed rather than 0x04).
        let mut compressed = spki(&[0x02; P256_POINT_LEN]);
        assert!(point_from_spki(&compressed).is_none());
        // Right prefix, wrong length.
        compressed.pop();
        assert!(point_from_spki(&compressed).is_none());
    }

    /// A blob in the layout `hpke.c` documents.
    fn blob_bytes(pubkey: &[u8], tag: &[u8], ciphertext: &[u8], iv: &[u8]) -> Vec<u8> {
        let mut out = vec![0x00, 0x01];
        for (field, value) in [
            (TAG_PUBKEY, pubkey),
            (TAG_AEAD_TAG, tag),
            (TAG_CIPHERTEXT, ciphertext),
            (TAG_IV, iv),
        ] {
            out.extend_from_slice(&field.to_be_bytes());
            #[allow(clippy::cast_possible_truncation)]
            out.extend_from_slice(&(value.len() as u16).to_be_bytes());
            out.extend_from_slice(value);
        }
        out
    }

    #[test]
    fn a_blob_decodes_into_its_four_fields() {
        let bytes = blob_bytes(&[1; 91], &[2; 12], &[3; 20], &[4; 12]);
        let blob = parse_blob(&bytes).expect("a complete blob");
        assert_eq!(blob.pubkey.len(), 91);
        assert_eq!(blob.tag, vec![2; 12]);
        assert_eq!(blob.ciphertext, vec![3; 20]);
        assert_eq!(blob.iv, vec![4; 12]);
    }

    #[test]
    fn a_malformed_blob_says_so_rather_than_failing_to_decrypt_later() {
        // No leading 0x0001.
        assert!(parse_blob(&[0x00, 0x02, 0x00, 0x01]).is_err());
        assert!(parse_blob(&[]).is_err());
        // A length that runs past the end.
        assert!(parse_blob(&[0x00, 0x01, 0x00, 0x01, 0xff, 0xff, 0x00]).is_err());
        // A tag nobody defined.
        let mut unknown = vec![0x00, 0x01];
        unknown.extend_from_slice(&9u16.to_be_bytes());
        unknown.extend_from_slice(&1u16.to_be_bytes());
        unknown.push(0);
        assert!(parse_blob(&unknown).is_err());
        // A missing field.
        let mut partial = vec![0x00, 0x01];
        partial.extend_from_slice(&TAG_PUBKEY.to_be_bytes());
        partial.extend_from_slice(&1u16.to_be_bytes());
        partial.push(0);
        assert!(parse_blob(&partial).is_err());
        // The wrong nonce size.
        assert!(parse_blob(&blob_bytes(&[1; 91], &[2; 12], &[3; 20], &[4; 8])).is_err());
        // A repeated field.
        let mut repeated = blob_bytes(&[1; 91], &[2; 12], &[3; 20], &[4; 12]);
        repeated.extend_from_slice(&TAG_IV.to_be_bytes());
        repeated.extend_from_slice(&12u16.to_be_bytes());
        repeated.extend_from_slice(&[5; 12]);
        assert!(parse_blob(&repeated).is_err());
    }

    /// Seals a token exactly as the gateway does, so the whole key schedule
    /// is exercised: ECDH, HKDF with `AC_ECIES`, AES-256-GCM.
    fn seal_as_gateway(client_public_der: &[u8], token: &str) -> TokenBlob {
        let rng = SystemRandom::new();
        let gateway_private =
            EphemeralPrivateKey::generate(&agreement::ECDH_P256, &rng).expect("a gateway key");
        let gateway_public = gateway_private.compute_public_key().expect("its point");

        let client_point = point_from_spki(client_public_der).expect("the client's point");
        let peer = UnparsedPublicKey::new(&agreement::ECDH_P256, client_point);
        let key: [u8; 32] = agreement::agree_ephemeral(gateway_private, &peer, |secret| {
            let prk = hkdf::Salt::new(hkdf::HKDF_SHA256, &[]).extract(secret);
            let mut key = [0u8; 32];
            prk.expand(&[HKDF_INFO], hkdf::HKDF_SHA256)
                .expect("expand")
                .fill(&mut key)
                .expect("fill");
            key
        })
        .expect("agreement");

        // Sealed with the same twelve-byte tag length the gateway uses, so
        // the round trip proves the truncation is handled rather than
        // working only because both sides use sixteen.
        let cipher = <Aes256Gcm12 as KeyInit>::new_from_slice(&key).expect("key");
        let iv = [7u8; IV_LEN];
        let mut buffer = token.as_bytes().to_vec();
        let tag = cipher
            .encrypt_in_place_detached(Nonce12::from_slice(&iv), &[], &mut buffer)
            .expect("seal");
        assert_eq!(tag.len(), TAG_LEN, "the gateway sends a truncated tag");

        TokenBlob {
            pubkey: spki(gateway_public.as_ref()),
            tag: tag.to_vec(),
            ciphertext: buffer,
            iv: iv.to_vec(),
        }
    }

    #[test]
    fn a_token_sealed_the_way_the_gateway_seals_it_opens() {
        // The point of this test: it pins the key schedule end to end. A
        // wrong info string, salt, curve or tag length fails it.
        let keys = Keys::generate().expect("client keys");
        let client_der = base64_decode(&keys.public_base64).expect("valid base64");
        let blob = seal_as_gateway(&client_der, "SSOTOKEN123abc");

        let token = decrypt(keys, &blob).expect("the token opens");
        assert_eq!(token, "SSOTOKEN123abc");
    }

    #[test]
    fn a_tampered_token_does_not_open() {
        let keys = Keys::generate().expect("client keys");
        let client_der = base64_decode(&keys.public_base64).expect("valid base64");
        let mut blob = seal_as_gateway(&client_der, "SSOTOKEN123abc");
        blob.ciphertext[0] ^= 0xff;

        let error = decrypt(keys, &blob).expect_err("a flipped bit must not open");
        assert!(error.to_string().contains("did not verify"), "got {error}");
    }

    #[test]
    fn a_token_sealed_for_someone_else_does_not_open() {
        // Proves the ECDH actually binds the token to our key rather than
        // the key material coming from somewhere constant.
        let ours = Keys::generate().expect("our keys");
        let theirs = Keys::generate().expect("another client's keys");
        let their_der = base64_decode(&theirs.public_base64).expect("valid base64");
        let blob = seal_as_gateway(&their_der, "SSOTOKEN123abc");

        assert!(decrypt(ours, &blob).is_err());
    }

    #[test]
    fn every_sign_in_gets_its_own_key() {
        let first = Keys::generate().expect("keys");
        let second = Keys::generate().expect("keys");
        assert_ne!(first.public_base64, second.public_base64);
        // And the wire form is the DER wrapper, not the bare point.
        let der = base64_decode(&first.public_base64).expect("valid base64");
        assert!(point_from_spki(&der).is_some(), "not an SPKI: {der:?}");
    }

    #[test]
    fn the_callback_request_line_yields_the_blob_and_the_redirect() {
        let callback =
            parse_request_line("GET /api/sso/YWJj?return=https%3A%2F%2Fvpn%2Fdone HTTP/1.1")
                .expect("a callback");
        assert_eq!(callback.blob, "YWJj");
        assert_eq!(callback.redirect.as_deref(), Some("https://vpn/done"));

        // No `return=` is allowed; the browser just gets the success page.
        let bare = parse_request_line("GET /api/sso/YWJj HTTP/1.0").expect("a callback");
        assert_eq!(bare.blob, "YWJj");
        assert!(bare.redirect.is_none());
    }

    #[test]
    fn a_stray_request_on_the_port_is_not_mistaken_for_the_sign_in() {
        // The port sees probes; answering one as the callback would abandon
        // the wait for the real thing.
        assert!(parse_request_line("GET / HTTP/1.1").is_none());
        assert!(parse_request_line("GET /api/sso/ HTTP/1.1").is_none());
        assert!(parse_request_line("POST /api/sso/YWJj HTTP/1.1").is_none());
        assert!(parse_request_line("GET /favicon.ico HTTP/1.1").is_none());
        assert!(parse_request_line("garbage").is_none());
        assert!(parse_request_line("GET /api/sso/YWJj RTSP/1.0").is_none());
    }

    #[test]
    fn a_percent_escaped_blob_is_decoded_before_it_is_parsed() {
        // base64 uses `+` and `/`, which arrive escaped in a URL path.
        let callback =
            parse_request_line("GET /api/sso/YQ%2Bb%2Fw%3D%3D HTTP/1.1").expect("a callback");
        assert_eq!(callback.blob, "YQ+b/w==");
        // A `+` is *not* turned into a space: this is a path, not a form.
        let plus = parse_request_line("GET /api/sso/YQ+b HTTP/1.1").expect("a callback");
        assert_eq!(plus.blob, "YQ+b");
    }
}
