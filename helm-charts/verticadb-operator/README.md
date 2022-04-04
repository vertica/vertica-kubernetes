This helm chart will install the operator and an admission controller webhook.  The following table describes the configuration parameters for this chart.  Refer to the helm documentation on how to set these parameters.

| Parameter Name | Description | Default Value |
|----------------|-------------|---------------|
| image.name | The name of image that runs the operator. | vertica/verticadb-operator:1.0.0 |
| image.repo | Repo server hosting image.name | null (DockerHub) |
| image.pullPolicy | The pull policy for the image that runs the operator  | IfNotPresent |
| rbac_proxy_image.name | Image name of Kubernetes RBAC proxy. | kubebuilder/kube-rbac-proxy:v0.8.0 |
| rbac_proxy_image.repo | Repo server hosting rbac_proxy_image.name | gcr.io |
| imagePullSecrets | List of Secret names containing login credentials for above repos | null (pull images anonymously) |
| webhook.caBundle | A PEM encoded CA bundle that will be used to validate the webhook's server certificate.  If unspecified, system trust roots on the apiserver are used. | |
| webhook.tlsSecret | The webhook requires a TLS certficate to work.  By default we rely on cert-manager to be installed as we use it generate the cert.  If you don't want to use cert-manager, you need to specify your own cert, which you can do with this parameter.  When set, it is a name of a secret in the same namespace the chart is being installed in.  The secret must have the keys: tls.key, ca.crt, and tls.crt. | |
| webhook.enable | If true, the webhook will be enabled and its configuration is setup by the helm chart. Setting this to false will disable the webhook. The webhook setup needs privileges to add validatingwebhookconfiguration and mutatingwebhookconfiguration, both are cluster scoped. If you do not have necessary privileges to add these configurations, then this option can be used to skip that and still deploy the operator. | true |
| logging.filePath | The path to the log file. If omitted, all logging will be written to stdout.  | |
| logging.maxFileSize | The maximum size, in MB, of the logging file before log rotation occurs. This is only applicable if logging to a file. | 500 |
| logging.maxFileAge | The maximum number of days to retain old log files based on the timestamp encoded in the file. This is only applicable if logging to a file. |
| logging.maxFileRotation | The maximum number of files that are kept in rotation before the old ones are removed. This is only applicable if logging to a file. | 3 |
| logging.level | The minimum logging level. Valid values are: debug, info, warn, and error | info |
| logging.dev | Enables development mode if true and production mode otherwise. | false |
| serviceAccountNameOverride | If set, this will be the name of an existing service account that will be used to run any of the pods related to this operator. This includes the pod for the operator itself, as well as any pods created for our custom resource. The necessary roles and role bindings must be already setup for this service account. If unset, we will use the default service account name and create the necessary roles and role bindings to allow the pods to interact with the apiserver. | |
| resources.\* | The resource requirements for the operator pod. | <pre>limits:<br>  cpu: 100m<br>  memory: 750Mi<br>requests:<br>  cpu: 100m<br>  memory: 20Mi</pre> |

