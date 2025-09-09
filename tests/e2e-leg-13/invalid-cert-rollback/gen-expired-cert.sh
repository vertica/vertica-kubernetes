#!/bin/bash
set -euo pipefail

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

cat > $TMPDIR/openssl.cnf <<EOF
[ ca ]
default_ca = CA_default

[ CA_default ]
dir               = $TMPDIR/demoCA
database          = \$dir/index.txt
new_certs_dir     = \$dir/newcerts
serial            = \$dir/serial
private_key       = \$dir/ca.key
certificate       = \$dir/ca.crt
default_md        = sha256
policy            = policy_any
email_in_dn       = no
copy_extensions   = copy
default_days      = 365

[ policy_any ]
commonName = supplied

[ req ]
distinguished_name = req_distinguished_name
prompt = no

[ req_distinguished_name ]
CN = expired.example.com

[ ext ]
extendedKeyUsage = serverAuth, clientAuth
EOF

mkdir -p $TMPDIR/demoCA/newcerts
touch $TMPDIR/demoCA/index.txt
echo 1000 > $TMPDIR/demoCA/serial

# Generate CA key and self-signed cert
openssl genpkey -algorithm RSA -out $TMPDIR/demoCA/ca.key
openssl req -x509 -new -nodes -key $TMPDIR/demoCA/ca.key -sha256 -days 3650 \
  -subj "/CN=demo-ca" \
  -out $TMPDIR/demoCA/ca.crt

# Generate key and CSR for expired cert
openssl genpkey -algorithm RSA -out $TMPDIR/expired.key
openssl req -new -key $TMPDIR/expired.key -out $TMPDIR/expired.csr -config $TMPDIR/openssl.cnf

# Sign the expired cert with custom start/end dates and EKU extensions
openssl ca -config $TMPDIR/openssl.cnf -in $TMPDIR/expired.csr -out $TMPDIR/expired.crt \
  -startdate "$(date -u -d '10 days ago' +%Y%m%d%H%M%SZ)" \
  -enddate "$(date -u -d '5 days ago' +%Y%m%d%H%M%SZ)" \
  -extensions ext -extfile $TMPDIR/openssl.cnf -batch

# Output Kubernetes TLS secret YAML to stdout
cat <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: expired-cert
type: kubernetes.io/tls
data:
  tls.crt: $(base64 -w0 < $TMPDIR/expired.crt)
  tls.key: $(base64 -w0 < $TMPDIR/expired.key)
EOF