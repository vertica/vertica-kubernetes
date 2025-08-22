SERVER_TLS_MODE=$(kubectl exec -n $NAMESPACE v-server-tls-mode-sc1-0 -c server -- vsql -tAc "select mode FROM tls_configurations WHERE name='server';")
if [[ $SERVER_TLS_MODE != "VERIFY_CA" ]]; then
    echo "ERROR: SERVER TLS MODE is not VERIFY_CA: $SERVER_TLS_MODE"
    exit 1
fi
exit 0
