This adds a new e2e test suite that we will use for vclusterOps integration. It doesn't do much to start, but we will add to it as we get further along with the vclusterOps integration.

You need to run with the next generation vertica-k8s image (docker-vertica-v2).

To run just this new test suite:

E2E_TEST_DIRS=tests/e2e-vcluster make run-int-tests