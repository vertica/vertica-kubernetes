K8S_LOCAL_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-client-server-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_local_tls_builtin_auth';")
if [[ $K8S_LOCAL_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_local_tls_builtin_auth not enabled"
    exit 1
fi
K8S_REMOTE_IPv4_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-client-server-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_remote_ipv4_tls_builtin_auth';")
if [[ $K8S_REMOTE_IPv4_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_remote_ipv4_tls_builtin_auth not enabled"
    exit 1
fi
K8S_REMOTE_IPv6_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-client-server-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_remote_ipv6_tls_builtin_auth';")
if [[ $K8S_REMOTE_IPv6_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_remote_ipv6_tls_builtin_auth not enabled"
    exit 1
fi
CERT_NAME=$(kubectl exec -n $NAMESPACE v-client-server-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select certificate from tls_configurations where name='server' and owner='dbadmin';")
if [[ $CERT_NAME != "server_cert_1" ]]; then
    echo "ERROR: cert server_cert_1 not found"
    exit 1
fi
CA_CERT_NAME=$(kubectl exec -n $NAMESPACE v-client-server-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select ca_certificate from tls_configurations where name='server' and owner='dbadmin';")
if [[ $CA_CERT_NAME != "server_ca_cert_1" ]]; then
    echo "ERROR: cert server_ca_cert_1 not found"
    exit 1
fi
KEY_NAME=$(kubectl exec -n $NAMESPACE v-client-server-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select name FROM cryptographic_keys WHERE secret_name='custom-cert';")
if [[ $KEY_NAME != "server_key_1" ]]; then
    echo "ERROR: key server_key_1 not found"
    exit 1
fi
exit 0
