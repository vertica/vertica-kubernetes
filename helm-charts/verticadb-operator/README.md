This helm chart will install the operator and an admission controller webhook.  The following table describes the configuration parameters for this chart.  Refer to the helm documentation on how to set these parameters.

| Parameter Name | Description | Default Value |
|----------------|-------------|---------------|
| affinity | The [affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) parameter allows you to constrain the operator pod only to specific nodes. If this parameter is not set, then no affinity setting is used with the operator pod. | Not set |
| controllers.enable | This controls if controllers are enabled when running the operator. The controllers are the part of the operator that watches and acts on custom resources. This option is useful if you want to deploy the operator just as a webhook. This comes in handy when deploying the operator as the namespace scope | true |
| controllers.scope | Defines the scope of the operator. You can define one of two values: cluster or namespace.<br><br>When set to cluster, the operator is cluster scoped. This means it will watch for changes to any custom resource across all namespaces. This is the default deployment.<br><br>When set to namespace, the operator is cluster scope. The operator will only set up watches for the namespace it is deployed in. You can deploy the operator in multiple namespaces this way. However, the webhook can only be run once in the cluster. You can control running of the webhook with the webhook.enable option. | cluster |
| controllers.burstSize | This controls the burst size for even recording in the operator. Increasing this allows the controllers to record more events in a short period. | 100 |
| controllers.vdbMaxBackoffDuration | This controls the maximum backoff requeue duration (in milliseconds) for the vdb controller. Increase this value to reduce the requeue rate if you have multiple databases running and want a lower rate limit. | 1000 |
| controllers.sandboxMaxBackoffDuration | This controls the maximum backoff requeue duration (in milliseconds) for the sandbox controller. Increase this value to reduce the requeue rate if you have multiple sandboxes running and want a lower rate limit. | 1000 |
| image.name | The name of image that runs the operator. | opentext/verticadb-operator:25.3.0-0 |
| image.repo | Repo server hosting image.name | docker.io |
| image.pullPolicy | The pull policy for the image that runs the operator  | IfNotPresent |
| imagePullSecrets | List of Secret names containing login credentials for above repos | null (pull images anonymously) |
| logging.filePath | The path to the log file. If omitted, all logging will be written to stdout.  | |
| logging.maxFileSize | The maximum size, in MB, of the logging file before log rotation occurs. This is only applicable if logging to a file. | |
| logging.maxFileAge | The maximum number of days to retain old log files based on the timestamp encoded in the file. This is only applicable if logging to a file. |
| logging.maxFileRotation | The maximum number of files that are kept in rotation before the old ones are removed. This is only applicable if logging to a file. | |
| logging.level | The minimum logging level. Valid values are: debug, info, warn, and error | info |
| logging.dev | Enables development mode if true and production mode otherwise. | false |
| nameOverride | Setting this allows you to control the prefix of all of the objects created by the helm chart.  If this is left blank, we use the name of the chart as the prefix | |
| nodeSelector | The [node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) provides control over which nodes are used to schedule a pod. If this parameter is not set, the node selector is omitted from the pod that is created by the operator's Deployment object. To set this parameter, provide a list of key/value pairs. | Not set |
| priorityClassName | The [priority class name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass) that is assigned to the operator pod. This affects where the pod gets scheduled. | Not set |
| prometheus.createProxyRBAC | Set this to false if you want to avoid creating the rbac rules for accessing the metrics endpoint when it is protected by the rbac auth proxy.  By default, we will create those RBAC rules. | true |
| prometheus.expose | Controls exposing of the prometheus metrics endpoint.  Valid options are:<br><br>- **EnableWithAuth**: A new service object will be created that exposes the metrics endpoint.  Access to the metrics are controlled by rbac rules. The metrics endpoint will use the https scheme.<br><br>- **EnableWithoutAuth**: Like EnableWithAuth, this will create a service object to expose the metrics endpoint.  However, there is no authority checking when using the endpoint.  Anyone who has network access to the endpoint (i.e. any pod in k8s) will be able to read the metrics.  The metrics endpoint will use the http scheme.<br><br>- **EnableWithTLS**: Like EnableWithAuth, this will create a service object to expose the metrics endpoint. However, there is no authority checking when using the endpoint. People with network access to the endpoint (i.e., any pod in Kubernetes) and the correct certificates can read the metrics. The metrics endpoint will use HTTPS and must be used with `tlsSecret`. If `tlsSecret` is not set, the behavior will be similar to `EnableWithoutAuth`, except that the endpoint will use HTTPS.<br><br>- **Disable**: Prometheus metrics are not exposed at all.  | Disable |
| prometheus.tlsSecret | Use this if you want to provide your own certs for the prometheus metrics endpoint. It refers to a secret in the same namespace that the helm chart is deployed in.  The secret must have the following keys set:<br><br>- **tls.key** – private key<br>- **tls.crt** – cert for the private key<br>- **ca.crt** – CA certificate<br><br>The prometheus.expose=EnableWithAuth must be set for the operator to use the certs provided. If this field is omitted, the operator will generate its own self-signed cert. | "" |
| reconcileConcurrency.eventtrigger | Set this to control the concurrency of reconciliations of EventTrigger CRs | 1 |
| reconcileConcurrency.sandboxconfigmap | Set this to control the concurrency of reconciliations of ConfigMaps that contain state for a sandbox  | 1 |
| reconcileConcurrency.verticaautoscaler | Set this to control the concurrency of reconciliations of VerticaAutoscaler CRs | 1 |
| reconcileConcurrency.verticadb | Set this to control the concurrency of reconciliations of VerticaDB CRs | 5 |
| reconcileConcurrency.verticarestorepointsquery | Set this to control the concurrency of reconciliations of VerticaRestorePointsQuery CRs | 1 |
| reconcileConcurrency.verticascrutinize | Set this to control the concurrency of reconciliations of VerticaScrutinize CRs | 1 |
| reconcileConcurrency.verticareplicator | Set this to control the concurrency of reconciliations of VerticaReplicator CRs | 3 |
| resources.\* | The resource requirements for the operator pod. | <pre>limits:<br>  cpu: 100m<br>  memory: 750Mi<br>requests:<br>  cpu: 100m<br>  memory: 20Mi</pre> |
| serviceAccountAnnotations | A map of annotations that will be added to the serviceaccount created. | |
| serviceAccountNameOverride | Controls the name given to the serviceaccount that is created. | |
| tolerations | Any [tolerations and taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) used to influence where a pod is scheduled. This parameter is provided as a list. | Not set |
| webhook.certSource | The webhook requires a TLS certificate to work. This parm defines how the cert is supplied. Valid values are:<br><br>- **internal**: The certs are generated internally by the operator prior to starting the managing controller. The generated cert is self-signed. When it expires, the operator pod will need to be restarted in order to generate a new certificate. This is the default.<br><br>- **cert-manager**: The certs are generated using the cert-manager operator.  This operator needs to be deployed before deploying the operator. Deployment of this chart will create a self-signed cert through cert-manager. The advantage of this over 'internal' is that cert-manager will automatically handle private key rotation when the certificate is about to expire.<br><br>- **secret**: The certs are created prior to installation of this chart and are provided to the operator through a secret. This option gives you the most flexibility as it is entirely up to you how the cert is created.  This option requires the webhook.tlsSecret option to be set. For backwards compatibility, if webhook.tlsSecret is set, it is implicit that this mode is selected. | internal |
| webhook.tlsSecret | The webhook requires a TLS certficate to work. By default we create a cert internally. If you want full control over the cert that is created you can use this parameter to provide it. When set, it is a name of a secret in the same namespace the chart is being installed in.  The secret must have the keys: tls.key and tls.crt. It can also include the key ca.crt. When that key is included the operator will patch it in the CA bundle in the webhook configuration.| |
| webhook.enable | If true, the webhook will be enabled and its configuration is setup by the helm chart. Setting this to false will disable the webhook. The webhook setup needs privileges to add validatingwebhookconfiguration and mutatingwebhookconfiguration, both are cluster scoped. If you do not have necessary privileges to add these configurations, then this option can be used to skip that and still deploy the operator. | true |
| cache.enable | If set to true, the operator will use cache to store tls certificates. Setting this to false will disable cache in the operator
| securityContext | Holds pod-level security attributes and common container settings. | <pre>fsGroup: 65532 <br>runAsGroup: 65532<br>runAsNonRoot: true <br>runAsUser: 65532 <br>seccompProfile:<br>  type: RuntimeDefault</pre> |
| containerSecurityContext | Defines the security options the manager container should be run with. | <pre>allowPrivilegeEscalation: false <br>readOnlyRootFilesystem: true <br>capabilities:<br>  drop: <br>  - ALL</pre> |
| keda.createRBACRules | Controls the creation of ClusterRole rules for KEDA objects. | true |

&nbsp;  
&nbsp; 

This table below describes monitoring configuration parameters including Grafana, Prometheus and Loki:

| Parameter Name | Description | Default Value |
|----------------|-------------|---------------|
| grafana.enabled | Deploy Grafana as part of the chart | false |
| grafana.namespaceOverride | Override the namespace where Grafana is deployed | "" |
| grafana.replicas | Number of grafana pods | 1 |
| grafana.adminUser | Username for Grafana admin user | admin |
| grafana.adminPassword | Password for Grafana admin user | admin |
| grafana.admin.existingSecret | Name of the secret. Can be templated. | "" |
| grafana.admin.userKey | The name of the field that contains the username in the secret | admin-user |
| grafana.admin.passwordKey | The name of the field that contains the password in the secret | admin-password |
| grafana.persistence | Control persistent storage for Grafana: ref: https://kubernetes.io/docs/concepts/storage/persistent-volumes/ |  |
| grafana.grafana.ini | Grafana's primary configuration. ref: http://docs.grafana.org/installation/configuration/ | |
| grafana.service | Expose the grafana service to be accessed from outside the cluster (LoadBalancer service). or access it from within the cluster (ClusterIP service). Set the service type and the port to serve it. | <pre>service:<br>  enabled: true<br>  type: ClusterIP<br>  ipFamilyPolicy: ""<br>  ipFamilies: []<br>  loadBalancerIP: ""<br>  loadBalancerClass: ""<br>  port: 80<br>  targetPort: 3000<br>  annotations: {}<br>  labels: {}<br>  portName: http-web<br>  appProtocol: ""<br>  sessionAffinity: ""</pre> |
| prometheusServer.enabled | Deploy Prometheus server as part of the chart | false |
| prometheusServer.prometheus.serviceAccount.create | Control whether a serviceaccount must be created with the required permissions | false |
| prometheusServer.prometheus.serviceAccount.name | Name of the serviceAccount | prometheus-vertica-sa (this is the static name of the service account the operator will generate from a template, if "create" is false) |
| prometheusServer.prometheus.serviceAccount.annotations | Annotations to add to the serviceAccount | {} |
| prometheusServer.prometheus.serviceAccount.automountServiceAccountToken | Control whether the service account’s token is automatically mounted into the pod | true |
| prometheusServer.prometheus.service | Configuration for Prometheus service0 ref: https://artifacthub.io/packages/helm/prometheus-community/kube-prometheus-stack?modal=values&path=prometheus.service | |
| prometheusServer.prometheus.prometheusSpec.replicas | Number of Prometheus replicas | 1 |
| prometheusServer.prometheus.prometheusSpec.retention | How long Prometheus should retain data | 7d |
| prometheusServer.prometheus.prometheusSpec.retentionSize | Max storage size before Prometheus starts deleting old data | 2GB |
| prometheusServer.prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage | Size of Prometheus persistent volume | 5Gi |
| prometheusServer.prometheus.web | WebTLSConfig defines the TLS parameters for HTTPS. ref: https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/api-reference/api.md#webtlsconfig | {} |
| prometheusServer.prometheusOperator.enabled | Enable Prometheus Operator (required for Prometheus) | true |
| prometheusServer.prometheusOperator.admissionWebhooks.enabled | Enable admission webhooks for Prometheus Operator | false |
| prometheusServer.defaultRules.create | Create default recording/alerting rules | false |
| loki.enabled | Deploy Loki as part of the chart | false |
| loki.loki.compactor.retention_enabled | Enable log retention | false |
| loki.loki.limits_config.retention_period | Set the global retention period | 720h |
| loki.loki.commonConfig.replication_factor | Stores multiple copies of logs in the ingester component | 3 |
| loki.loki.schemaConfig.configs.object_store | Type of object storage for schema config | s3 |
| loki.loki.storage.type | Storage for Loki chunks | s3 |
| loki.minio.enabled | Whether to use minio as the object storage backend | true |
| loki.lokiCanary.enabled | The Loki canary pushes logs to and queries from this loki installation to test that it's working correctly | true |
| loki.test.enabled | To test if a Loki data source is enabled and working | true |
| alloy.enabled | Deploy Alloy as part of the chart | false |
| alloy.replicaCount | Define the number of replicas for the Alloy deployment | 3 |
| alloy.configMap.create | Whether to create a new ConfigMap for the config file | true |
| alloy.configMap.name | Name of existing ConfigMap to use when configMap.create is false | |
| alloy.configMap.key | Key in ConfigMap to get config from when using existing ConfigMap | |
| alloy.rbac.create | Whether to create RBAC resources for Alloy | true |
| alloy.serviceAccount.create | Whether to create a service account for Alloy | true |
| alloy.serviceAccount.name | The name of the existing service account to use when serviceAccount.create is false | |
