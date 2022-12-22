## Test Suites

This is the root directory of all of the integration style tests. They are named e2e, which stands for end-to-end. Each directory that starts with e2e- is a test suite. We categorize the test suites in the following ways:

1. e2e-leg-_x_: This is a particular leg of a test suite, where _x_ is a number. The test can run without any special requirements. We run these in the CI using various communal backends (e.g. minio/S3, azure, HDFS, etc.)
2. e2e-udx: These test UDx capabilities with the vertica-k8s image. This requires special setup to create the test environment. We run UDx samples, taken from a separate location.
3. e2e-server-upgrade: These tests focus on testing the operator when upgrading the vertica server.
4. e2e-operator-upgrade-overlays: These tests focus on testing upgrade of the operator itself. It needs a special environment because we need to install old versions of the operator before upgrading to the current version.
5. e2e-operator-upgrade-template: This isn't a test suite per say. It provides a template to generate operator upgrade tests in e2e-operator-upgrade-overlays.
6. e2e-http-server: These test focus on testing the http server within vertica. This needs a new version of Vertica, which is why it's not include in the e2e-leg-x suites.

## Running Tests Individually

Any of the tests can be run individually using the following syntax:

```
kubectl kuttl test --test <testname>
```

Where `<testname>` is a directory in any of the test suites (e.g. http-custom-certs).
