This will drive restart by killing the primary statefulset.  This adds test
coverage for the case when it needs to reform the cluster but the statefulset
for the primary subcluster doesn't exist.
