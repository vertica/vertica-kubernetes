CERT_NAME=$(kubectl exec -n $NAMESPACE v-just-client-server-auth-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select certificate from tls_configurations where name='server' and owner='dbadmin';")
if [[ $CERT_NAME != "server_cert_1" ]]; then
    echo "ERROR: cert server_cert_1 not found"
    exit 1
fi
CA_CERT_NAME=$(kubectl exec -n $NAMESPACE v-just-client-server-auth-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select ca_certificate from tls_configurations where name='server' and owner='dbadmin';")
if [[ $CA_CERT_NAME != "server_ca_cert_1" ]]; then
    echo "ERROR: cert server_ca_cert_1 not found"
    exit 1
fi
KEY_NAME=$(kubectl exec -n $NAMESPACE v-just-client-server-auth-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select name FROM cryptographic_keys WHERE secret_name='custom-cert';")
if [[ $KEY_NAME != "server_key_1" ]]; then
    echo "ERROR: key server_key_1 not found"
    exit 1
fi
exit 0
