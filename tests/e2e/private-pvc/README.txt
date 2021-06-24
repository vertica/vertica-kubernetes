Tests the deployment against a provisioner that creates the PVC with private
(0775) access.  We use a version of Rancher's local-path-provisioner,
configured so that the PV is created with 0775 permissions.
