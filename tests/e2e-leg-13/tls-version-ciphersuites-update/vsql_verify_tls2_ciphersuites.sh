TLS_VERSION=$(kubectl exec -n $NAMESPACE v-tls-ver-cipersuites-sc1-0 -c nma -- vsql -U dbadmin -w '' -tAc "SELECT get_config_parameter('MinTLSVersion');")
if [[ ! $TLS_VERSION == "2" ]]; then
    echo "ERROR: TLS version is not 2: $TLS_VERSION"
    exit 1
fi
TLS2_CIPHER_SUITES=$(kubectl exec -n $NAMESPACE v-tls-ver-cipersuites-sc1-0 -c nma -- vsql -U dbadmin -w '' -tAc "SELECT get_config_parameter('enabledciphersuites')")
if [[ $TLS2_CIPHER_SUITES != "ECDHE-RSA-AES256-GCM-SHA384,ECDHE-RSA-AES128-SHA" ]]; then
    echo "ERROR: TLS 2 cipher suite is not ECDHE-RSA-AES256-GCM-SHA384,ECDHE-RSA-AES128-SHA: $TLS2_CIPHER_SUITES"
    exit 1
fi
exit 0