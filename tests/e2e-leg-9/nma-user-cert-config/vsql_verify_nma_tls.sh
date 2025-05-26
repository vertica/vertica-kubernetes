NMA_TLS_SECRET_NAME=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select secret_name from certificates where name='https_cert_0';")
if [[ $NMA_TLS_SECRET_NAME != "nma-cert" ]]; then
    echo "ERROR: nma TLS secret name is not nma-cert"
    exit 1
fi
HTTPS_TLS_MODE=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select mode FROM tls_configurations WHERE name='https';")
if [[ $HTTPS_TLS_MODE != "VERIFY_CA" ]]; then
    echo "ERROR: HTTPS TLS mode is not VERIFY_CA"
    exit 1
fi
exit 0
