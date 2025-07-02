awk '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/ { print } NR > 1 && /-----END CERTIFICATE-----/ { exit }' $2 > $2_cut
kubectl get secret/custom-cert -n $1 -o jsonpath='{.data.tls\.crt}'  | base64 --decode > /tmp/secret_cert.txt
diff $2_cut  /tmp/secret_cert.txt
exit $?