HTTPS_TLS_MODE=$(kubectl exec -n $NAMESPACE v-tls-certs-sc1-0 -c server -- vsql -tAc "select mode FROM tls_configurations WHERE name='https';")
if [[ $HTTPS_TLS_MODE != "VERIFY_CA" ]]; then
    echo "ERROR: HTTPS TLS MODE is not VERIFY_CA: $HTTPS_TLS_MODE"
    exit 1
fi
exit 0
