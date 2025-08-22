K8S_LOCAL_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-cert-rotate-rollback-after-update-sc1-0 -c server -- vsql -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_local_tls_builtin_auth';")
if [[ $K8S_LOCAL_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_local_tls_builtin_auth not enabled"
    exit 1
fi
K8S_REMOTE_IPv4_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-cert-rotate-rollback-after-update-sc1-0 -c server -- vsql -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_remote_ipv4_tls_builtin_auth';")
if [[ $K8S_REMOTE_IPv4_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_remote_ipv4_tls_builtin_auth not enabled"
    exit 1
fi
K8S_REMOTE_IPv6_TLS_ENABLED=$(kubectl exec -n $NAMESPACE v-cert-rotate-rollback-after-update-sc1-0 -c server -- vsql -tAc "SELECT is_auth_enabled FROM client_auth where auth_name='k8s_remote_ipv6_tls_builtin_auth';")
if [[ $K8S_REMOTE_IPv6_TLS_ENABLED != "True" ]]; then
    echo "ERROR: k8s_remote_ipv6_tls_builtin_auth not enabled"
    exit 1
fi
CERT_NAME=$(kubectl exec -n $NAMESPACE v-cert-rotate-rollback-after-update-sc1-0 -c server -- vsql -tAc "select certificate from tls_configurations where name='https' and owner='dbadmin';")
if [[ $CERT_NAME != "https_cert_2" ]]; then
    echo "ERROR: cert https_cert_2 not found"
    exit 1
fi
CA_CERT_NAME=$(kubectl exec -n $NAMESPACE v-cert-rotate-rollback-after-update-sc1-0 -c server -- vsql -tAc "select ca_certificate from tls_configurations where name='https' and owner='dbadmin';")
if [[ $CA_CERT_NAME != "https_ca_cert_2" ]]; then
    echo "ERROR: cert https_ca_cert_2 not found"
    exit 1
fi
KEY_NAME=$(kubectl exec -n $NAMESPACE v-cert-rotate-rollback-after-update-sc1-0 -c server -- vsql -tAc "select max(name) FROM cryptographic_keys WHERE secret_name='start-cert';")
if [[ $KEY_NAME != "https_key_2" ]]; then
    echo "ERROR: key https_key_2 not found"
    exit 1
fi
exit 0
