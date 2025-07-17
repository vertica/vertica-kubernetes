HTTPS_TLS_MODE=$(kubectl exec -n $NAMESPACE v-tls-mode-rotate-rollback-sc1-0 -c server -- vsql -tAc "select mode FROM tls_configurations WHERE name='https';")
if [[ $HTTPS_TLS_MODE != "TRY_VERIFY" ]]; then
    echo "ERROR: HTTPS TLS MODE is not TRY_VERIFY"
    exit 1
fi
exit 0
