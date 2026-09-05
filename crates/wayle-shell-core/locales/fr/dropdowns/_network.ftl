### Menu déroulant du réseau

dropdown-network-title = Réseau
dropdown-network-active-connections = Connexions actives
dropdown-network-active-connection = Connexion active
dropdown-network-available = Réseaux disponibles
dropdown-network-connected = Connecté
dropdown-network-connecting = Connexion
dropdown-network-connect = Connecter
dropdown-network-disconnect = Déconnecter
dropdown-network-forget = Oublier
dropdown-network-dismiss = Fermer
dropdown-network-error = Erreur
dropdown-network-cancel = Annuler
dropdown-network-password-placeholder = Entrez le mot de passe
dropdown-network-ethernet = Ethernet
dropdown-network-wifi = Wi-Fi
dropdown-network-no-networks-title = Aucun réseau trouvé
dropdown-network-no-networks-description = Assurez-vous que le Wi-Fi est activé et relancez la recherche
dropdown-network-no-adapter-title = Aucun adaptateur Wi-Fi
dropdown-network-no-adapter-description = Aucun adaptateur sans fil n'a été détecté sur ce système

## Types de sécurité

dropdown-network-security-open = Ouvert
dropdown-network-security-wep = WEP
dropdown-network-security-wpa = WPA
dropdown-network-security-wpa2 = WPA2
dropdown-network-security-wpa3 = WPA3
dropdown-network-security-enterprise = Entreprise
dropdown-network-security-saved = { $security } · Enregistré

## Étapes de connexion

dropdown-network-step-preparing = Préparation…
dropdown-network-step-configuring = Configuration…
dropdown-network-step-authenticating = Authentification…
dropdown-network-step-obtaining-ip = Obtention de l'adresse IP…
dropdown-network-step-verifying = Vérification de la connexion…

## Erreurs de connexion

dropdown-network-error-wrong-password = Échec de l'authentification
dropdown-network-error-timeout = Délai de connexion dépassé
dropdown-network-error-ip-config = Impossible d'obtenir une adresse IP
dropdown-network-error-not-found = Réseau introuvable
dropdown-network-error-generic = Échec de la connexion

## VPN

dropdown-network-vpn = VPN
dropdown-network-vpn-connected = Connecté
dropdown-network-vpn-connecting = Connexion...
dropdown-network-vpn-disconnected = Déconnecté
dropdown-network-vpn-failed = Échec de la connexion

## Credentials

dropdown-network-secret-title = Authentification requise
dropdown-network-secret-submit = Valider
dropdown-network-secret-password = Mot de passe
dropdown-network-secret-username = Nom d'utilisateur
dropdown-network-secret-pin = Code PIN
dropdown-network-secret-group = Groupe
dropdown-network-secret-domain = Domaine
dropdown-network-secret-wep-key = Clé WEP
dropdown-network-secret-private-key = Clé privée
dropdown-network-secret-private-key-password = Mot de passe de la clé privée

## VPN configuration

dropdown-network-vpn-add = Ajouter un VPN
dropdown-network-vpn-edit = Modifier
dropdown-network-vpn-new = Nouveau VPN
dropdown-network-vpn-name = Nom
dropdown-network-vpn-type = Type
dropdown-network-vpn-save = Enregistrer
dropdown-network-vpn-delete = Supprimer
dropdown-network-vpn-delete-confirm = Supprimer { $name } ?
dropdown-network-vpn-delete-confirm-detail = Cette action est irréversible. Une clé privée enregistrée ici n'est stockée nulle part ailleurs.
dropdown-network-vpn-no-native-sign-in = connexion via le greffon
dropdown-network-vpn-import = Importer un fichier wg-quick
dropdown-network-vpn-import-filter = Configuration WireGuard
dropdown-network-vpn-import-failed = Ce fichier n'est pas une configuration wg-quick
dropdown-network-vpn-generate-key = Générer une nouvelle paire de clés
dropdown-network-vpn-public-key = Clé publique : { $key }
dropdown-network-vpn-public-key-empty = La clé publique apparaît ici une fois la clé privée définie. Transmettez-la à la personne qui gère l'autre extrémité.
dropdown-network-vpn-raw-hint = Une paire clé = valeur par ligne
dropdown-network-vpn-name-required = Un nom est requis
dropdown-network-vpn-field-required = { $field } est requis
dropdown-network-vpn-invalid-text = { $field } n'est pas valide
dropdown-network-vpn-invalid-host = { $field } doit être un nom d'hôte ou une adresse, sans https:// ni chemin
dropdown-network-vpn-invalid-host-port = { $field } doit être un hôte et un port, comme vpn.example.com:51820
dropdown-network-vpn-invalid-ip-list = { $field } doit être des adresses IP, séparées par des virgules
dropdown-network-vpn-invalid-cidr-list = { $field } doit être des adresses comme 10.0.0.2/24, séparées par des virgules
dropdown-network-vpn-invalid-key = { $field } doit être une clé WireGuard : 44 caractères en base64
dropdown-network-vpn-invalid-number = { $field } doit être un nombre entier
dropdown-network-vpn-section-interface = Cette machine
dropdown-network-vpn-section-addressing = Adressage
dropdown-network-vpn-section-peer = Pair
dropdown-network-vpn-field-interface = Interface
dropdown-network-vpn-field-private-key = Clé privée
dropdown-network-vpn-field-address = Adresses
dropdown-network-vpn-field-dns = DNS
dropdown-network-vpn-field-peer-public-key = Clé publique du pair
dropdown-network-vpn-field-peer-endpoint = Point de terminaison du pair
dropdown-network-vpn-field-peer-allowed-ips = IP autorisées
dropdown-network-vpn-field-peer-preshared-key = Clé partagée
dropdown-network-vpn-field-peer-keepalive = Maintien de connexion (secondes)
dropdown-network-vpn-field-gateway = Passerelle
dropdown-network-vpn-field-protocol = Protocole
dropdown-network-vpn-field-wayle-username = Nom d'utilisateur
dropdown-network-vpn-field-remote = Serveur
dropdown-network-vpn-field-username = Nom d'utilisateur
dropdown-network-vpn-field-password = Mot de passe
dropdown-network-vpn-field-ca = Certificat CA

## Sections de formulaire VPN
dropdown-network-vpn-section-gateway = Passerelle
dropdown-network-vpn-section-credentials = Connexion
dropdown-network-vpn-section-certificates = Certificats
dropdown-network-vpn-section-cipher = Chiffrement
dropdown-network-vpn-section-ipsec = IPsec

## Éditeur de clés brut
dropdown-network-vpn-advanced-show = Clés avancées
dropdown-network-vpn-advanced-hide = Masquer les clés avancées
dropdown-network-vpn-back = Retour
dropdown-network-vpn-raw-hint-typed = Une paire clé = valeur par ligne, pour les clés que ce formulaire ne propose pas. Déjà couvertes : { $covered }
dropdown-network-vpn-raw-hint-unknown = Une paire clé = valeur par ligne. Elles sont transmises à { $service } telles quelles — consultez la documentation de ce greffon pour les clés qu'il accepte.

## Champs des greffons VPN
dropdown-network-vpn-field-ca-cert = Certificat CA
dropdown-network-vpn-field-cert = Certificat utilisateur
dropdown-network-vpn-field-certificate = Certificat de la passerelle
dropdown-network-vpn-field-cert-pass = Mot de passe de la clé privée
dropdown-network-vpn-field-connection-type = Authentification
dropdown-network-vpn-field-domain = Domaine
dropdown-network-vpn-field-fragsize = Taille de fragment
dropdown-network-vpn-field-ike-dh-group = Groupe DH IKE
dropdown-network-vpn-field-ikev2 = Version IKE
dropdown-network-vpn-field-ipsec-enabled = IPsec
dropdown-network-vpn-field-ipsec-gateway = Passerelle
dropdown-network-vpn-field-ipsec-gateway-id = Identifiant de la passerelle
dropdown-network-vpn-field-ipsec-id = Nom du groupe
dropdown-network-vpn-field-ipsec-psk = Clé partagée
dropdown-network-vpn-field-ipsec-secret = Mot de passe du groupe
dropdown-network-vpn-field-key = Clé privée
dropdown-network-vpn-field-leftid = Identité locale
dropdown-network-vpn-field-leftxauthusername = Nom d'utilisateur
dropdown-network-vpn-field-method = Authentification
dropdown-network-vpn-field-nameserver = Serveur de noms
dropdown-network-vpn-field-nat-traversal-mode = Traversée de NAT
dropdown-network-vpn-field-otp = Code à usage unique
dropdown-network-vpn-field-perfect-forward-secrecy = Confidentialité persistante
dropdown-network-vpn-field-pskvalue = Clé partagée
dropdown-network-vpn-field-realm = Domaine d'authentification
dropdown-network-vpn-field-require-mppe = Exiger le chiffrement
dropdown-network-vpn-field-right = Passerelle
dropdown-network-vpn-field-topdomain = Domaine racine
dropdown-network-vpn-field-trusted-cert = Certificat de confiance
dropdown-network-vpn-field-user = Nom d'utilisateur
dropdown-network-vpn-field-usercert = Certificat utilisateur
dropdown-network-vpn-field-userkey = Clé privée
dropdown-network-vpn-field-wayle-sso = Connexion par navigateur (SAML)
dropdown-network-vpn-field-xauth-password = Mot de passe
dropdown-network-vpn-field-xauthpassword = Mot de passe
dropdown-network-vpn-field-xauth-username = Nom d'utilisateur
