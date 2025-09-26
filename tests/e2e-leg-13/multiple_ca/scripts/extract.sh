kubectl get secret $1 -n $NAMESPACE -o jsonpath='{.data.ca\.crt}' | base64 --decode  > /tmp/certs/$1_ca.crt
kubectl get secret $1 -n $NAMESPACE -o jsonpath='{.data.tls\.crt}' | base64 --decode > /tmp/certs/$1.crt
kubectl get secret $1 -n $NAMESPACE -o jsonpath='{.data.tls\.key}' | base64 --decode > /tmp/certs/$1.key
