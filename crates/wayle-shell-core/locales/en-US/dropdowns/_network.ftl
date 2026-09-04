### Network Dropdown

dropdown-network-title = Network
dropdown-network-active-connections = Active Connections
dropdown-network-active-connection = Active Connection
dropdown-network-available = Available Networks
dropdown-network-connected = Connected
dropdown-network-connecting = Connecting
dropdown-network-connect = Connect
dropdown-network-disconnect = Disconnect
dropdown-network-forget = Forget
dropdown-network-dismiss = Dismiss
dropdown-network-error = Error
dropdown-network-cancel = Cancel
dropdown-network-password-placeholder = Enter password
dropdown-network-ethernet = Ethernet
dropdown-network-wifi = WiFi
dropdown-network-no-networks-title = No Networks Found
dropdown-network-no-networks-description = Make sure WiFi is enabled and try scanning again
dropdown-network-no-adapter-title = No WiFi Adapter
dropdown-network-no-adapter-description = No wireless adapter was detected on this system

## Security Types

dropdown-network-security-open = Open
dropdown-network-security-wep = WEP
dropdown-network-security-wpa = WPA
dropdown-network-security-wpa2 = WPA2
dropdown-network-security-wpa3 = WPA3
dropdown-network-security-enterprise = Enterprise
dropdown-network-security-saved = { $security } · Saved

## Connection Steps

dropdown-network-step-preparing = Preparing...
dropdown-network-step-configuring = Configuring...
dropdown-network-step-authenticating = Authenticating...
dropdown-network-step-obtaining-ip = Obtaining IP address...
dropdown-network-step-verifying = Verifying connection...

## Connection Errors

dropdown-network-error-wrong-password = Authentication failed
dropdown-network-error-timeout = Connection timed out
dropdown-network-error-ip-config = Failed to obtain IP address
dropdown-network-error-not-found = Network not found
dropdown-network-error-generic = Connection failed

## VPN

dropdown-network-vpn = VPN
dropdown-network-vpn-connected = Connected
dropdown-network-vpn-connecting = Connecting...
dropdown-network-vpn-disconnected = Disconnected
dropdown-network-vpn-failed = Connection failed

## Credentials

dropdown-network-secret-title = Authentication required
dropdown-network-secret-submit = Submit
dropdown-network-secret-password = Password
dropdown-network-secret-username = Username
dropdown-network-secret-pin = PIN
dropdown-network-secret-group = Group
dropdown-network-secret-domain = Domain
dropdown-network-secret-wep-key = WEP key
dropdown-network-secret-private-key = Private key
dropdown-network-secret-private-key-password = Private key password

## VPN configuration

dropdown-network-vpn-add = Add VPN
dropdown-network-vpn-edit = Edit
dropdown-network-vpn-new = New VPN
dropdown-network-vpn-name = Name
dropdown-network-vpn-type = Type
dropdown-network-vpn-save = Save
dropdown-network-vpn-delete = Delete
dropdown-network-vpn-delete-confirm = Delete { $name }?
dropdown-network-vpn-delete-confirm-detail = This cannot be undone. A private key stored here is not saved anywhere else.
dropdown-network-vpn-no-native-sign-in = signs in through the plugin
dropdown-network-vpn-import = Import a wg-quick file
dropdown-network-vpn-import-filter = WireGuard configuration
dropdown-network-vpn-import-failed = That file is not a wg-quick configuration
dropdown-network-vpn-raw-hint = One key = value per line
dropdown-network-vpn-raw-hint-typed = One key = value per line, for keys this form has no box for. Already covered: { $covered }
dropdown-network-vpn-raw-hint-unknown = One key = value per line. These go to { $service } exactly as written — see that plugin's own documentation for the keys it accepts.
dropdown-network-vpn-advanced-show = Advanced keys
dropdown-network-vpn-advanced-hide = Hide advanced keys
dropdown-network-vpn-name-required = A name is required
dropdown-network-vpn-field-required = { $field } is required
dropdown-network-vpn-invalid-text = { $field } is not valid
dropdown-network-vpn-invalid-host = { $field } should be a hostname or an address, with no https:// and no path
dropdown-network-vpn-invalid-host-port = { $field } should be a host and a port, like vpn.example.com:51820
dropdown-network-vpn-invalid-ip-list = { $field } should be IP addresses, separated by commas
dropdown-network-vpn-invalid-cidr-list = { $field } should be addresses like 10.0.0.2/24, separated by commas
dropdown-network-vpn-invalid-key = { $field } should be a WireGuard key: 44 characters of base64
dropdown-network-vpn-invalid-number = { $field } should be a whole number
dropdown-network-vpn-section-interface = This machine
dropdown-network-vpn-section-addressing = Addressing
dropdown-network-vpn-section-peer = Peer
dropdown-network-vpn-field-interface = Interface
dropdown-network-vpn-field-private-key = Private key
dropdown-network-vpn-field-address = Addresses
dropdown-network-vpn-field-dns = DNS
dropdown-network-vpn-field-peer-public-key = Peer public key
dropdown-network-vpn-field-peer-endpoint = Peer endpoint
dropdown-network-vpn-field-peer-allowed-ips = Allowed IPs
dropdown-network-vpn-field-peer-preshared-key = Preshared key
dropdown-network-vpn-field-peer-keepalive = Keepalive (seconds)
dropdown-network-vpn-field-gateway = Gateway
dropdown-network-vpn-field-protocol = Protocol
dropdown-network-vpn-field-wayle-username = Username
dropdown-network-vpn-field-remote = Server
dropdown-network-vpn-field-username = Username
dropdown-network-vpn-field-password = Password
dropdown-network-vpn-field-ca = CA certificate

# Sections shared by the plugin forms
dropdown-network-vpn-section-gateway = Gateway
dropdown-network-vpn-section-credentials = Sign-in
dropdown-network-vpn-section-certificates = Certificates
dropdown-network-vpn-section-cipher = Encryption
dropdown-network-vpn-section-ipsec = IPsec

# Plugin fields. The id is the plugin's own key, slugified — vpnc's keys have
# spaces and capitals in them, so `IPSec gateway` becomes `ipsec-gateway`.
dropdown-network-vpn-field-cert = User certificate
dropdown-network-vpn-field-key = Private key
dropdown-network-vpn-field-cert-pass = Private key password
dropdown-network-vpn-field-connection-type = Authentication
dropdown-network-vpn-field-ipsec-gateway = Gateway
dropdown-network-vpn-field-ipsec-id = Group name
dropdown-network-vpn-field-ipsec-secret = Group password
dropdown-network-vpn-field-xauth-username = Username
dropdown-network-vpn-field-xauth-password = Password
dropdown-network-vpn-field-domain = Domain
dropdown-network-vpn-field-nat-traversal-mode = NAT traversal
dropdown-network-vpn-field-ike-dh-group = IKE DH group
dropdown-network-vpn-field-perfect-forward-secrecy = Forward secrecy
dropdown-network-vpn-field-certificate = Gateway certificate
dropdown-network-vpn-field-method = Authentication
dropdown-network-vpn-field-user = Username
dropdown-network-vpn-field-usercert = User certificate
dropdown-network-vpn-field-userkey = Private key
dropdown-network-vpn-field-right = Gateway
dropdown-network-vpn-field-leftid = Local identity
dropdown-network-vpn-field-ikev2 = IKE version
dropdown-network-vpn-field-leftxauthusername = Username
dropdown-network-vpn-field-xauthpassword = Password
dropdown-network-vpn-field-pskvalue = Pre-shared key
dropdown-network-vpn-field-ipsec-enabled = IPsec
dropdown-network-vpn-field-ipsec-psk = Pre-shared key
dropdown-network-vpn-field-ipsec-gateway-id = Gateway ID
dropdown-network-vpn-field-require-mppe = Require encryption
dropdown-network-vpn-field-ca-cert = CA certificate
dropdown-network-vpn-field-trusted-cert = Trusted certificate
dropdown-network-vpn-field-otp = One-time code
dropdown-network-vpn-field-realm = Realm
dropdown-network-vpn-field-topdomain = Top domain
dropdown-network-vpn-field-nameserver = Nameserver
dropdown-network-vpn-field-fragsize = Fragment size
dropdown-network-vpn-field-wayle-sso = Browser sign-in (SAML)
dropdown-network-vpn-back = Back
