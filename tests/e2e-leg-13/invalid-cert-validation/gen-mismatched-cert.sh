#!/bin/bash

set -euo pipefail

SECRET_NAME="mismatched-cert"
TMP_DIR=$(mktemp -d)

# Generate first key/cert pair
openssl genrsa -out "$TMP_DIR/key1.pem" 2048
openssl req -x509 -new -nodes -key "$TMP_DIR/key1.pem" -sha256 -days 365 -out "$TMP_DIR/cert1.pem" -subj "/CN=cert-one"

# Generate second key (this one won't match cert1.pem)
openssl genrsa -out "$TMP_DIR/key2.pem" 2048

# Base64 encode cert and mismatched key
CERT_B64=$(base64 -w 0 "$TMP_DIR/cert1.pem")
KEY_B64=$(base64 -w 0 "$TMP_DIR/key2.pem")

# Output a Kubernetes TLS Secret with mismatched cert/key
cat <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: $SECRET_NAME
  namespace: $NAMESPACE
type: kubernetes.io/tls
data:
  tls.crt: $CERT_B64
  tls.key: $KEY_B64
EOF