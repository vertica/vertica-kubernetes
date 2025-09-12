cat /tmp/certs/cert1_ca.crt /tmp/certs/cert2_ca.crt /tmp/certs/cert3_ca.crt > /tmp/certs/combined_raw
base64 -i /tmp/certs/combined_raw > /tmp/certs/combined_base64
tr -d "\n" < /tmp/certs/combined_base64 > /tmp/certs/combined_singleline
kubectl patch secret cert1 -n $NAMESPACE --type='json'  -p="[{\"op\" : \"replace\" ,\"path\" : \"/data/ca.crt\", \"value\": \"$(cat /tmp/certs/combined_singleline)\"}]"
