This helm chart will install the operator and an admission controller webhook.  The following table describes the configuration parameters for this chart.  Refer to the helm documentation on how to set these parameters.

| Parameter Name | Description | Default Value |
|----------------|-------------|---------------|
| image.name | The name of image that runs the operator. | vertica/verticadb-operator:1.0.0 |
| webhook.caBundle | A PEM encoded CA bundle that will be used to validate the webhook's server certificate.  If unspecified, system trust roots on the apiserver are used. | |
| webhook.tlsSecret | The webhook requires a TLS certficate to work.  By default we rely on cert-manager to be installed as we use it generate the cert.  If you don't want to use cert-manager, you need to specify your own cert, which you can do with this parameter.  When set, it is a name of a secret in the same namespace the chart is being installed in.  The secret must have the keys: tls.key, ca.crt, and tls.crt. | |
| logging.filePath | The path to the log file. If omitted, all logging will be written to stdout.  | |
| logging.maxFileSize | The maximum size, in MB, of the logging file before log rotation occurs. This is only applicable if logging to a file. | 500 |
| logging.maxFileAge | The maximum number of days to retain old log files based on the timestamp encoded in the file. This is only applicable if logging to a file. |
| logging.maxFileRotation | The maximum number of files that are kept in rotation before the old ones are removed. This is only applicable if logging to a file. | 3 |
| logging.level | The minimum logging level. Valid values are: debug, info, warn, and error | info |
| logging.dev | Enables development mode if true and production mode otherwise. | false |
| resources.\* | The resource requirements for the operator pod. | <pre>limits:<br>  cpu: 100m<br>  memory: 750Mi<br>requests:<br>  cpu: 100m<br>  memory: 20Mi</pre> |

