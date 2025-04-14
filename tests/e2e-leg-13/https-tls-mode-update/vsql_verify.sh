K8S_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_tls_builtin_auth';")
if [[ $K8S_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_tls_builtin_auth not enabled"
    exit 1
fi
CERT_NAME=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select certificate from tls_configurations where name='https' and owner='dbadmin';")
if [[ $CERT_NAME != "https_cert_1" ]]; then
    echo "ERROR: cert https_cert_1 not found"
    exit 1
fi
CA_CERT_NAME=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select ca_certificate from tls_configurations where name='https' and owner='dbadmin';")
if [[ $CA_CERT_NAME != "https_ca_cert_1" ]]; then
    echo "ERROR: cert https_ca_cert_1 not found"
    exit 1
fi
KEY_NAME=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select name FROM cryptographic_keys WHERE secret_name='custom-cert';")
if [[ $KEY_NAME != "https_key_1" ]]; then
    echo "ERROR: key https_key_1 not found"
    exit 1
fi
HTTPS_TLS_MODE=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select mode FROM tls_configurations WHERE name='https';")
if [[ $KEY_NAME != "https_key_1" ]]; then
    echo "ERROR: key https_key_1 not found"
    exit 1
fi
exit 0
