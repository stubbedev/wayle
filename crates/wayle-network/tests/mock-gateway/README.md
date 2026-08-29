# Mock VPN gateways

Three throwaway gateways for the VPN sign-in tests, so nothing has to be
pointed at a real VPN to prove the sign-in works:

- `127.0.0.1:8443` — GlobalProtect: username/password, one challenge round,
  then a cookie;
- `127.0.0.1:8444` — a GlobalProtect SAML portal, which wayle must refuse
  before posting any credentials at it;
- `127.0.0.1:8445` — AnyConnect: an XML form, a challenge, then a `webvpn`
  cookie. It refuses any reply that does not echo its `<opaque>` blob, which is
  what the real ones do.

```sh
just test-gateway     # up, run the `mock::` tests, down again
```

The tests are `#[ignore]`d in the normal suite (`mod mock` in
`crates/wayle-network/src/vpn/openconnect/gp.rs` and `anyconnect.rs`) because
they need these containers running.

## The certificates

`gateway.crt` is a leaf signed by `ca.crt`, valid for `127.0.0.1` and
`localhost`. The tests trust `ca.crt` through `SSL_CERT_FILE` and verify the
gateway for real — the point of the exercise is the `gwcert` secret, which is
the pin of whatever certificate the connection actually presented.

Because they are committed, the expected pin is a constant in the tests. To
reissue them (they expire in 2036), regenerate both and update `PIN`:

```sh
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -subj "/CN=wayle mock gateway CA" -keyout ca.key -out ca.crt
openssl req -newkey rsa:2048 -nodes -subj "/CN=wayle-mock-gateway" \
  -keyout gateway.key -out gateway.csr
openssl x509 -req -in gateway.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -days 3650 -out gateway.crt -extfile <(printf \
  "subjectAltName=IP:127.0.0.1,DNS:localhost\nbasicConstraints=CA:FALSE\n\
keyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n")
rm ca.key gateway.csr ca.srl

openssl x509 -in gateway.crt -pubkey -noout \
  | openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | base64
```

`gateway.key` is a private key in a public repository on purpose: it protects a
container that answers a fixed script on loopback and holds nothing.
