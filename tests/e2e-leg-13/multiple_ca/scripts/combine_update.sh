cat /tmp/certs/$1_ca.crt /tmp/certs/$2_ca.crt /tmp/certs/$3_ca.crt > /tmp/certs/combined_$1_raw
base64 -i /tmp/certs/combined_$1_raw > /tmp/certs/combined_$1_base64
tr -d "\n" < /tmp/certs/combined_$1_base64 > /tmp/certs/combined_$1_singleline
kubectl patch secret $1 -n $NAMESPACE --type='json'  -p="[{\"op\" : \"replace\" ,\"path\" : \"/data/ca.crt\", \"value\": \"$(cat /tmp/certs/combined_$1_singleline)\"}]"
