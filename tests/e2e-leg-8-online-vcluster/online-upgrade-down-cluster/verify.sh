pod=$(kubectl get pod -n $NAMESPACE | grep operator | cut -f 1 -d " ")
kubectl logs $pod -n $NAMESPACE  | grep "Restarting vertica in primary subclusters"
exit $?
