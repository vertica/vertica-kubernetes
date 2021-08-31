This helm chart will install the operator and an admission controller webhook.  The following table describes the configuration parameters for this chart.  Refer to the helm documentation on how to set these parameters.

| Parameter Name | Description | Default Value |
|----------------|-------------|---------------|
| image.name | The name of image that runs the operator. | vertica/verticadb-operator:1.0.0 |
| webhook.caBundle | A PEM encoded CA bundle that will be used to validate the webhook's server certificate.  If unspecified, system trust roots on the apiserver are used. | |
| webhook.tlsSecrets | The webhook requires a TLS certficate to work.  By default we rely on cert-manager to be installed as we use it generate the cert.  If you don't want to use cert-manager, you need to specify your own cert, which you can do with this parameter.  When set, it is a name of a secret in the same namespace the chart is being installed in.  The secret must have the keys: tls.key, ca.crt, and tls.crt. | |

