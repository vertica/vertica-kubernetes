MODE=$(kubectl exec -n $NAMESPACE v-inter-node-cert-rotate-sc1-0 -c server -- vsql -U dbadmin -w superuser -tAc "select mode from tls_configurations where name='data_channel' and owner='dbadmin';")
if [[ $MODE != "TRY_VERIFY" ]]; then
    echo "ERROR: found mode $MODE; expected mode TRY_VERIFY"
    exit 1
fi
exit 0
