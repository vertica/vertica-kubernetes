COUNT=$(kubectl get secret $1 -n $NAMESPACE -o jsonpath='{.data.ca\.crt}' | base64 --decode | grep -c "BEGIN CERTIFICATE")
if [ $COUNT -eq 3 ]
then 
    exit 0
else 
    exit 1
fi
