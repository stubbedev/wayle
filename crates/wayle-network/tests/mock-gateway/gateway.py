#!/usr/bin/env python3
"""A GlobalProtect gateway, in as much detail as wayle's sign-in can tell.

Speaks the two endpoints `crates/wayle-network/src/vpn/openconnect/gp.rs`
talks to, over TLS with the certificate next to this file — the sign-in reads
the peer certificate off that connection to produce the `gwcert` secret, so a
plaintext mock would not exercise the thing most worth exercising.

MODE picks which gateway this is:

  form  — username/password, then one challenge round, then a cookie.
  saml  — a portal that answers prelogin with a SAML redirect, which wayle
          must refuse *before* posting any credentials at it.

Nothing here is a fixture of a real gateway's traffic; the response shapes come
from openconnect's `auth-globalprotect.c`, which is also where gp.rs got them.
"""

import os
import ssl
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs

HERE = os.path.dirname(os.path.abspath(__file__))

USER = "alice"
PASSWORD = "hunter2"
CHALLENGE_TOKEN = "CHALLENGE-1"
CHALLENGE_ANSWER = "123456"
COOKIE = "AUTHCOOKIEVALUE"

PRELOGIN_FORM = """<?xml version="1.0" encoding="UTF-8" ?>
<prelogin-response><status>Success</status><ccusername></ccusername>
<autosubmit>false</autosubmit><msg></msg><newmsg></newmsg><license>yes</license>
<authentication-message>Sign in to the mock gateway</authentication-message>
<username-label>Company ID</username-label><password-label>Passphrase</password-label>
<panos-version>2</panos-version><saml-default-browser>yes</saml-default-browser>
<auth-api>no</auth-api><region>DK</region></prelogin-response>"""

PRELOGIN_SAML = """<?xml version="1.0" encoding="UTF-8" ?>
<prelogin-response><status>Success</status>
<saml-auth-method>REDIRECT</saml-auth-method>
<saml-request>aHR0cHM6Ly9pZHAuZXhhbXBsZS5jb20v</saml-request>
</prelogin-response>"""

CHALLENGE = f"""<?xml version="1.0" encoding="UTF-8" ?>
<challenge><respmsg>Approve the push on your phone</respmsg>
<inputstr>{CHALLENGE_TOKEN}</inputstr></challenge>"""

REJECTED = """<?xml version="1.0" encoding="UTF-8" ?>
<response status="error"><msg>Invalid username or password</msg></response>"""


def success(computer: str) -> str:
    """The positional argument list a real gateway answers a good login with."""
    arguments = [
        "",
        COOKIE,
        "0123456789abcdef",
        "127.0.0.1",
        USER,
        "LDAP-auth",
        "vsys1",
        "example",
        "",
        "",
        "",
        "",
        "tunnel",
        "-1",
        "4100",
        "",
        "PORTALCOOKIE",
        "",
        "",
        "4",
        "unknown",
    ]
    body = "".join(
        f"<argument>{value}</argument>" if value else "<argument/>" for value in arguments
    )
    _ = computer
    return f'<?xml version="1.0" encoding="UTF-8"?><jnlp><application-desc>{body}</application-desc></jnlp>'


class Gateway(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format, *args):  # noqa: A002 - the base class names it
        sys.stderr.write("mock-gateway: " + format % args + "\n")

    def _reply(self, body: str, status: int = 200) -> None:
        encoded = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/xml")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def do_GET(self) -> None:
        if not self.path.startswith("/ssl-vpn/prelogin.esp"):
            self._reply("<html>not a gateway</html>", 404)
            return
        mode = os.environ.get("MODE", "form")
        self._reply(PRELOGIN_SAML if mode == "saml" else PRELOGIN_FORM)

    def do_POST(self) -> None:
        if not self.path.startswith("/ssl-vpn/login.esp"):
            self._reply("<html>not a gateway</html>", 404)
            return

        length = int(self.headers.get("Content-Length", "0"))
        form = parse_qs(self.rfile.read(length).decode(), keep_blank_values=True)
        field = lambda key: form.get(key, [""])[0]  # noqa: E731

        if field("user") != USER:
            self._reply(REJECTED)
            return

        # First post carries the password and no challenge token; the answer to
        # the challenge comes back in the same `passwd` field.
        if not field("inputStr"):
            self._reply(CHALLENGE if field("passwd") == PASSWORD else REJECTED)
            return

        if field("inputStr") == CHALLENGE_TOKEN and field("passwd") == CHALLENGE_ANSWER:
            self._reply(success(field("computer")))
            return
        self._reply(REJECTED)


def main() -> None:
    port = int(os.environ.get("PORT", "8443"))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(
        os.path.join(HERE, "gateway.crt"), os.path.join(HERE, "gateway.key")
    )

    server = ThreadingHTTPServer(("0.0.0.0", port), Gateway)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    sys.stderr.write(f"mock-gateway: listening on {port} in {os.environ.get('MODE', 'form')} mode\n")
    server.serve_forever()


if __name__ == "__main__":
    main()
