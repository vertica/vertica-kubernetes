TLS_VERSION=$(kubectl exec -n $NAMESPACE v-tls-ver-cipersuites-sc1-0 -c nma -- vsql -U dbadmin -w '' -tAc "SELECT get_config_parameter('MinTLSVersion');")
if [[ ! $TLS_VERSION == "3" ]]; then
    echo "ERROR: TLS version is not 3: $TLS_VERSION"
    exit 1
fi

TLS3_CIPHER_SUITES=$(kubectl exec -n $NAMESPACE v-tls-ver-cipersuites-sc1-0 -c nma -- vsql -U dbadmin -w '' -tAc "SELECT get_config_parameter('tlsciphersuites')")
# TLS3_CIPHER_SUITES=$(echo $TLS3_CIPHER_SUITES | xargs)
if [[ "$TLS3_CIPHER_SUITES" != "TLS_AES_256_GCM_SHA384" ]]; then
    echo "ERROR: TLS 3 cipher suite is not TLS_AES_256_GCM_SHA384: [$TLS3_CIPHER_SUITES]"
    exit 1
fi
exit 0