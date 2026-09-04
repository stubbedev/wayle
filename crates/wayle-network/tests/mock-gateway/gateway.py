#!/usr/bin/env python3
"""A VPN gateway, in as much detail as wayle's sign-in can tell.

Speaks the endpoints `crates/wayle-network/src/vpn/openconnect/` talks to —
GlobalProtect's two `.esp` endpoints and AnyConnect's XML exchange — over TLS
with the certificate next to this file. The sign-in reads the peer certificate
off that connection to produce the `gwcert` secret, so a plaintext mock would
not exercise the thing most worth exercising.

MODE picks which gateway this is:

  form        — GlobalProtect: username/password, one challenge round, cookie.
  saml        — a GlobalProtect portal that answers prelogin with a SAML
                redirect, which wayle must refuse *before* posting any
                credentials at it.
  anyconnect  — Cisco: an XML form, a challenge, then a `webvpn` cookie.
  fortinet    — FortiGate: an HTML login form posted to
                `/remote/logincheck`, a `tokeninfo` second factor, then an
                `SVPNCOOKIE`. Not XML at all.
  array       — Array Networks: one form POST, then an `ANsession…` cookie.
                No challenge round at all.

Nothing here is a fixture of a real gateway's traffic; the response shapes come
from openconnect's `auth-globalprotect.c`, `auth.c`, `fortinet.c` and
`array.c`, which is also where gp.rs, anyconnect.rs, fortinet.rs and array.rs
got them.
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

# Fortinet's bookkeeping, echoed back by the challenge round.
FORTI_REQID = "17"
FORTI_MAGIC = "deadbeef"
FORTI_COOKIE = "SVPNSESSIONVALUE"

# Array's session cookie. The name carries a varying suffix, which is why
# the client matches it by prefix.
ARRAY_COOKIE = "ARRAYSESSION"

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

OPAQUE = (
    '<opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup</tunnel-group>'
    "<config-hash>1699999999999</config-hash></opaque>"
)

AC_MAIN = f"""<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">
{OPAQUE}
<auth id="main"><title>Login</title>
<message>Please enter your username and password.</message>
<form><input type="text" name="username" label="Username:"/>
<input type="password" name="password" label="Password:"/>
<select name="group_list" label="GROUP:">
<option value="Employees" selected="true">Employees</option>
</select></form></auth></config-auth>"""

AC_CHALLENGE = f"""<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">
{OPAQUE}
<auth id="challenge"><message>Answer with the code from your token.</message>
<form><input type="password" name="secondary_password" label="Code:"/></form>
</auth></config-auth>"""

AC_SUCCESS = """<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="complete" aggregate-auth-version="2">
<auth id="success"><title>SSL VPN Service</title></auth>
<session-token>SESSIONTOKEN</session-token></config-auth>"""

AC_REJECTED = """<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">
<auth id="main"><error id="88" param1="">Login failed.</error>
<form><input type="text" name="username" label="Username:"/>
<input type="password" name="password" label="Password:"/></form>
</auth></config-auth>"""


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

    def _anyconnect(self, body: str) -> None:
        """One exchange of Cisco's XML authentication."""
        if "<config-auth" not in body:
            self._reply("<html>not a gateway</html>", 400)
            return
        if 'type="init"' in body:
            # The cookie a gateway clears before there is a session: wayle
            # must not mistake it for one.
            self.send_response(200)
            self.send_header("Content-Type", "application/xml")
            self.send_header(
                "Set-Cookie", "webvpn=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/"
            )
            encoded = AC_MAIN.encode()
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)
            return

        # Every reply has to echo the opaque blob back, or the gateway does
        # not recognise the conversation.
        if "<tunnel-group>DefaultWEBVPNGroup</tunnel-group>" not in body:
            self._reply(AC_REJECTED)
            return

        if f"<secondary_password>{CHALLENGE_ANSWER}</secondary_password>" in body:
            encoded = AC_SUCCESS.encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/xml")
            self.send_header("Set-Cookie", "webvpn=SESSIONVALUE; path=/; secure; HttpOnly")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)
            return

        if f"<username>{USER}</username>" in body and f"<password>{PASSWORD}</password>" in body:
            self._reply(AC_CHALLENGE)
            return
        self._reply(AC_REJECTED)

    def _fortinet(self, body: str) -> None:
        """One round of FortiGate's form authentication."""
        form = parse_qs(body, keep_blank_values=True)
        field = lambda key: form.get(key, [""])[0]  # noqa: E731

        if field("username") != USER:
            self._reply("ret=0,err=Permission denied")
            return

        # A challenge round sends `code` and echoes the values from the
        # previous reply. The real thing recognises the conversation by them,
        # so refusing without them is what makes the echo load-bearing.
        if "code" in form:
            if field("reqid") != FORTI_REQID or field("magic") != FORTI_MAGIC:
                self._reply("ret=0,err=Session not recognised")
                return
            if field("code") == CHALLENGE_ANSWER:
                self.send_response(200)
                self.send_header("Content-Type", "text/plain")
                self.send_header(
                    "Set-Cookie", f"SVPNCOOKIE={FORTI_COOKIE}; path=/; secure; HttpOnly"
                )
                self.send_header("Content-Length", "0")
                self.end_headers()
                return
            self._reply("ret=0,err=Wrong code")
            return

        # First round: the password buys a `tokeninfo` challenge.
        if field("credential") == PASSWORD:
            self._reply(
                f"ret=2,tokeninfo=,grp=Employees,reqid={FORTI_REQID},polid=3,"
                f"portal=web,peer=1,magic={FORTI_MAGIC},"
                "chal_msg=Enter your token code"
            )
            return
        self._reply("ret=0,err=Permission denied")

    def _array(self, body: str) -> None:
        """Array's one-shot login: no challenge, just a cookie or a refusal."""
        form = parse_qs(body, keep_blank_values=True)
        field = lambda key: form.get(key, [""])[0]  # noqa: E731

        # Array's own field names. Answering to `username`/`password` would
        # let a client using the wrong names pass, which is the mistake most
        # worth catching here.
        if field("uname") == USER and field("pwd") == PASSWORD:
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header(
                "Set-Cookie", f"ANsession1234={ARRAY_COOKIE}; path=/; secure; HttpOnly"
            )
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        self._reply("<html>Login failed</html>")

    def do_POST(self) -> None:
        mode = os.environ.get("MODE")
        if mode == "array":
            if not self.path.startswith("/prx/000/http/localhost/login"):
                self._reply("<html>not a gateway</html>", 404)
                return
            length = int(self.headers.get("Content-Length", "0"))
            self._array(self.rfile.read(length).decode())
            return
        if mode == "anyconnect":
            length = int(self.headers.get("Content-Length", "0"))
            self._anyconnect(self.rfile.read(length).decode())
            return
        if mode == "fortinet":
            if not self.path.startswith("/remote/logincheck"):
                self._reply("<html>not a gateway</html>", 404)
                return
            length = int(self.headers.get("Content-Length", "0"))
            self._fortinet(self.rfile.read(length).decode())
            return
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
