CERT_NAME=$(kubectl exec -n $NAMESPACE v-create-enable-rotate-add-sc1-0 -c server -- vsql -tAc "select certificate from tls_configurations where name='https' and owner='dbadmin';")
if [[ $CERT_NAME != "https_cert_1" ]]; then
    echo "ERROR: cert https_cert_1 not found"
    exit 1
fi
SECRET_NAME=$(kubectl exec -n $NAMESPACE v-create-enable-rotate-add-sc1-0 -c server -- vsql -tAc "select secret_name from certificates where name='https_cert_1';")
if [[ $SECRET_NAME != "custom-cert" ]]; then
    echo "ERROR: https_cert_1 is not using custom-cert"
    exit 1
fi
CERT_NAME=$(kubectl exec -n $NAMESPACE v-create-enable-rotate-add-sc1-0 -c server -- vsql -tAc "select certificate from tls_configurations where name='server' and owner='dbadmin';")
if [[ $CERT_NAME != "server_cert_0" ]]; then
    echo "ERROR: cert server_cert_0 not found"
    exit 1
fi
SECRET_NAME=$(kubectl exec -n $NAMESPACE v-create-enable-rotate-add-sc1-0 -c server -- vsql -tAc "select secret_name from certificates where name='server_cert_0';")
if [[ $SECRET_NAME != "custom-cert" ]]; then
    echo "ERROR: server_cert_0 is not using custom-cert"
    exit 1
fi
exit 0
