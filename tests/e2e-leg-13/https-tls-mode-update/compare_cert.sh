start=`grep -n BEGIN\ CERTIFICATE $2 | cut -f 1 -d:`
end=`grep -n END\ CERTIFICATE $2 | cut -f 1 -d:`
len="$((end-start+1))"
head -n $end $2 | tail -n $len > $2_cut
kubectl get secret/custom-cert -n $1 -o jsonpath='{.data.tls\.crt}'  | base64 --decode > /tmp/secret_cert.txt
diff $2_cut  /tmp/secret_cert.txt
exit $?
