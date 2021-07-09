This will test the UpdateStrategy for both kSafety 0 and kSafety 1.  For kSafety 0, we use the OnDelete for the update strategy, so all pods must be manually deleted.  For kSafety 1, we use the RollingUpdate, so all pods will come back automatically.

We test the updateStrategy by adding a license secret after the DB has been
created.  The license will get mounted in the container and vertica will be up
and running.
