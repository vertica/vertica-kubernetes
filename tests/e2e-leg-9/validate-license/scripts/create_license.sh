kubectl create secret generic $1 --from-file=license.dat=$2 --namespace $NAMESPACE
