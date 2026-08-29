//! The gateway certificate pin the openconnect plugin asks for as `gwcert`.
//!
//! `nm-openconnect` passes that secret straight to `openconnect --servercert`,
//! and the plugin's `need_secrets` counts the key as missing until it is
//! there — so a sign-in that returns only a cookie and a gateway never
//! launches openconnect at all, however valid the cookie is.
//!
//! Of the three forms `--servercert` accepts, only `pin-sha256:` is a hash of
//! the certificate itself; `sha1:`/`sha256:` are hex hashes of the *public
//! key*, which is why hashing the whole DER and prefixing it `sha256:` is
//! rejected. Verified against openconnect 9.12: the DER digest does not match
//! and the RFC 7469 pin does.
//!
//! The pin is over the certificate's `SubjectPublicKeyInfo`, so the DER is
//! walked far enough to find it — six elements into the TBSCertificate — and
//! no further. A full X.509 parser buys nothing here: the certificate has
//! already been validated by rustls against the system trust store before any
//! of this runs.

use base64::Engine;
use sha2::{Digest, Sha256};

/// DER tag for `SEQUENCE`/`SEQUENCE OF`.
const SEQUENCE: u8 = 0x30;

/// DER tag for the `[0] EXPLICIT` wrapper around `TBSCertificate.version`.
const VERSION_TAG: u8 = 0xA0;

/// The `TBSCertificate` fields between the (optional) version and the public
/// key: `serialNumber`, `signature`, `issuer`, `validity`, `subject`.
const FIELDS_BEFORE_KEY: usize = 5;

/// The `pin-sha256:` fingerprint of a DER-encoded certificate, or `None` when
/// the bytes are not a certificate this can read.
pub(super) fn pin(der: &[u8]) -> Option<String> {
    let key_info = subject_public_key_info(der)?;
    let digest = Sha256::digest(key_info);
    Some(format!(
        "pin-sha256:{}",
        base64::engine::general_purpose::STANDARD.encode(digest)
    ))
}

/// The `SubjectPublicKeyInfo` element of a certificate, header included —
/// RFC 7469 pins the whole encoded structure, not just the key bits.
fn subject_public_key_info(der: &[u8]) -> Option<&[u8]> {
    let certificate = sequence(der)?;
    let mut fields = sequence(certificate.value)?.value;

    // Version is `[0] EXPLICIT` and optional; a v1 certificate omits it and
    // starts straight at the serial number.
    let first = element(fields)?;
    if first.tag == VERSION_TAG {
        fields = first.rest;
    }
    for _ in 0..FIELDS_BEFORE_KEY {
        fields = element(fields)?.rest;
    }

    Some(sequence(fields)?.whole)
}

/// One DER element: its tag, the whole encoding, the value inside it, and
/// whatever follows.
struct Element<'a> {
    tag: u8,
    whole: &'a [u8],
    value: &'a [u8],
    rest: &'a [u8],
}

/// The element at `input`, when it is a `SEQUENCE`.
fn sequence(input: &[u8]) -> Option<Element<'_>> {
    element(input).filter(|element| element.tag == SEQUENCE)
}

/// Reads one DER element.
fn element(input: &[u8]) -> Option<Element<'_>> {
    let tag = *input.first()?;
    let (length, length_len) = self::length(input.get(1..)?)?;
    let header = length_len.checked_add(1)?;
    let total = header.checked_add(length)?;
    let whole = input.get(..total)?;
    Some(Element {
        tag,
        whole,
        value: whole.get(header..)?,
        rest: input.get(total..)?,
    })
}

/// Reads a DER length, returning it and how many bytes it occupied.
///
/// The indefinite form (`0x80`) is BER, not DER, and is refused rather than
/// guessed at.
fn length(input: &[u8]) -> Option<(usize, usize)> {
    let first = *input.first()?;
    if first < 0x80 {
        return Some((usize::from(first), 1));
    }
    let count = usize::from(first & 0x7f);
    if count == 0 || count > size_of::<usize>() {
        return None;
    }
    let value = input
        .get(1..=count)?
        .iter()
        .fold(0_usize, |accumulated, byte| {
            (accumulated << 8) | usize::from(*byte)
        });
    Some((value, count + 1))
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A self-signed P-256 certificate for `vpn.example.com`, and the pin
    /// `openssl x509 -pubkey | openssl pkey -pubin -outform der | sha256 |
    /// base64` computes for it. Small on purpose: a real gateway's chain adds
    /// kilobytes and tests nothing this does not.
    const CERTIFICATE: &str = "MIIBiDCCAS+gAwIBAgIULWz/JZGl3ygYikhOjj+qjSL0fc4wCgYIKoZIzj0EAwIwGjEYMBYGA1UE\
        AwwPdnBuLmV4YW1wbGUuY29tMB4XDTI2MDgyOTEyMTAyNVoXDTM2MDgyNjEyMTAyNVowGjEYMBYGA1UE\
        AwwPdnBuLmV4YW1wbGUuY29tMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEy4iFnIEy5Fr+/ZDgGzRe\
        DOqAzwtiFcfALwjht8WwwIJA7bjPRdw5+kuOo0xTLWaVklIzFnlsFpk+xMQw0TGvzKNTMFEwHQYDVR0O\
        BBYEFH9KHDQLlJ7mIv7F0EfaQmn+deh2MB8GA1UdIwQYMBaAFH9KHDQLlJ7mIv7F0EfaQmn+deh2MA8G\
        A1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDRwAwRAIgESQS805DfMcBRZMifgXqNsrxxxtH3sxb4nIE\
        scoQTQUCIHnxw4VWTcHPaqkPkBGCZgBaw+++iXkzvT86Ff+q0anr";

    const EXPECTED_PIN: &str = "pin-sha256:1AGrIgWOEB5Sxfg6xFFl5UEsy9ForMLzaTiwiRHc+Hw=";

    fn certificate() -> Vec<u8> {
        base64::engine::general_purpose::STANDARD
            .decode(CERTIFICATE.replace(['\n', ' '], ""))
            .expect("the fixture is valid base64")
    }

    #[test]
    fn a_certificate_pins_to_the_hash_openconnect_prints() {
        // Not a self-consistency check: this string came out of openssl, and
        // openconnect 9.12 accepted it for the same certificate.
        assert_eq!(pin(&certificate()).as_deref(), Some(EXPECTED_PIN));
    }

    #[test]
    fn the_pin_is_of_the_public_key_not_of_the_whole_certificate() {
        // The whole-DER digest is what `sha256:` would have meant, and it is
        // not what openconnect compares a `pin-sha256:` against.
        let whole = base64::engine::general_purpose::STANDARD.encode(Sha256::digest(certificate()));
        assert_ne!(pin(&certificate()), Some(format!("pin-sha256:{whole}")));
    }

    #[test]
    fn bytes_that_are_not_a_certificate_produce_no_pin() {
        // Silently pinning garbage would hand the plugin a value that only
        // fails once the tunnel is being brought up.
        assert_eq!(pin(&[]), None);
        assert_eq!(pin(b"not der at all"), None);
        assert_eq!(pin(&[SEQUENCE, 0x02, 0x01, 0x00]), None);
    }

    #[test]
    fn a_truncated_certificate_is_refused_rather_than_read_short() {
        let der = certificate();
        for cut in [der.len() / 2, der.len() - 1] {
            assert_eq!(pin(&der[..cut]), None, "truncated at {cut}");
        }
    }

    #[test]
    fn a_length_that_claims_more_than_it_has_is_refused() {
        // 0x82 says "two length bytes follow", claiming 0xffff bytes of value.
        assert_eq!(pin(&[SEQUENCE, 0x82, 0xff, 0xff, 0x00]), None);
        // The indefinite form is BER; DER does not have it.
        assert_eq!(length(&[0x80]), None);
        assert_eq!(length(&[0x01]), Some((1, 1)));
        assert_eq!(length(&[0x81, 0x80]), Some((128, 2)));
    }
}
