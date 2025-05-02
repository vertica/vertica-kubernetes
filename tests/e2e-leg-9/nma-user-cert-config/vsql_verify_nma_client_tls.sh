SERVER_TLS_SECRET_NAME=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select secret_name from certificates where name='server_cert';")
if [[ $SERVER_TLS_SECRET_NAME != "client-cert" ]]; then
    echo "ERROR: server TLS secret name is not client-cert"
    exit 1
fi
SERVER_TLS_MODE=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select mode FROM tls_configurations WHERE name='server';")
if [[ $SERVER_TLS_MODE != "VERIFY_CA" ]]; then
    echo "ERROR: server TLS mode is not VERIFY_CA"
    exit 1
fi
exit 0
