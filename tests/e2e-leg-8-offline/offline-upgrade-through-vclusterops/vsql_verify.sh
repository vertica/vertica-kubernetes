TLS_MODE=$(kubectl exec -n $NAMESPACE v-upgrade-vertica-defsc-0 -c server -- vsql -w 'topsecret' -tAc "SELECT mode FROM tls_configurations where name='server';")
if [[ $TLS_MODE != "TRY_VERIFY" ]]; then
    echo "ERROR: server TLS mode is not try_verify"
    exit 1
fi