/*
Copyright [2021-2023] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	DefaultS3Region       = "us-east-1"
	DefaultGCloudRegion   = "US-EAST1"
	DefaultGCloudEndpoint = "https://storage.googleapis.com"

	// Additional server config parameters
	S3SseKmsKeyID = "S3SseKmsKeyId"
)

// ExtractNamespacedName gets the name and returns it as a NamespacedName
func (v *VerticaDB) ExtractNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      v.ObjectMeta.Name,
		Namespace: v.ObjectMeta.Namespace,
	}
}

// MakeVDBName is a helper that creates a sample name for test purposes
func MakeVDBName() types.NamespacedName {
	return types.NamespacedName{Name: "vertica-sample", Namespace: "default"}
}

// FindTransientSubcluster will return a pointer to the transient subcluster if one exists
func (v *VerticaDB) FindTransientSubcluster() *Subcluster {
	for i := range v.Spec.Subclusters {
		if v.Spec.Subclusters[i].IsTransient {
			return &v.Spec.Subclusters[i]
		}
	}
	return nil
}

// MakeVDB is a helper that constructs a fully formed VerticaDB struct using the sample name.
// This is intended for test purposes.
func MakeVDB() *VerticaDB {
	nm := MakeVDBName()
	return &VerticaDB{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       VerticaDBKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			UID:         "abcdef-ghi",
			Annotations: make(map[string]string),
		},
		Spec: VerticaDBSpec{
			AutoRestartVertica: true,
			Labels:             make(map[string]string),
			Annotations:        make(map[string]string),
			Image:              "vertica-k8s:latest",
			InitPolicy:         CommunalInitPolicyCreate,
			Communal: CommunalStorage{
				Path:             "s3://nimbusdb/mspilchen",
				Endpoint:         "http://minio",
				CredentialSecret: "s3-auth",
			},
			Local: LocalStorage{
				DataPath:    "/data",
				DepotPath:   "/depot",
				DepotVolume: PersistentVolume,
				RequestSize: resource.MustParse("10Gi"),
			},
			KSafety:                  KSafety1,
			DBName:                   "db",
			ShardCount:               12,
			DeprecatedHTTPServerMode: HTTPServerModeDisabled,
			Subclusters: []Subcluster{
				{Name: "defaultsubcluster", Size: 3, ServiceType: corev1.ServiceTypeClusterIP, IsPrimary: true},
			},
		},
	}
}

// MakeVDBForHTTP is a helper that constructs a VerticaDB struct with http enabled.
// This is intended for test purposes.
func MakeVDBForHTTP(httpServerTLSSecretName string) *VerticaDB {
	vdb := MakeVDB()
	vdb.Annotations[vmeta.VersionAnnotation] = HTTPServerMinVersion
	vdb.Spec.DeprecatedHTTPServerMode = HTTPServerModeEnabled
	vdb.Spec.HTTPServerTLSSecret = httpServerTLSSecretName
	return vdb
}

// GenSubclusterMap will organize all of the subclusters into a map for quicker lookup
func (v *VerticaDB) GenSubclusterMap() map[string]*Subcluster {
	scMap := map[string]*Subcluster{}
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		scMap[sc.Name] = sc
	}
	return scMap
}

// IsValidSubclusterName validates the subcluster name is valid.  We have rules
// about its name because it is included in the name of the statefulset, so we
// must adhere to the Kubernetes rules for object names.
func IsValidSubclusterName(scName string) bool {
	r := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?$`)
	return r.MatchString(scName)
}

// HasReviveInstanceIDAnnotation is true when an annotation exists for the db's
// revive_instance_id.
func (v *VerticaDB) HasReviveInstanceIDAnnotation() bool {
	_, ok := v.ObjectMeta.Annotations[vmeta.ReviveInstanceIDAnnotation]
	return ok
}

// MergeAnnotations will merge new annotations with vdb.  It will return true if
// any annotation changed.  Caller is responsible for updating the Vdb in the
// API server.
func (v *VerticaDB) MergeAnnotations(newAnnotations map[string]string) bool {
	changedAnnotations := false
	for k, newValue := range newAnnotations {
		oldValue, ok := v.ObjectMeta.Annotations[k]
		if !ok || oldValue != newValue {
			if v.ObjectMeta.Annotations == nil {
				v.ObjectMeta.Annotations = map[string]string{}
			}
			v.ObjectMeta.Annotations[k] = newValue
			changedAnnotations = true
		}
	}
	return changedAnnotations
}

// GenInstallerIndicatorFileName returns the name of the installer indicator file.
// Valid only for the current instance of the vdb.
func (v *VerticaDB) GenInstallerIndicatorFileName() string {
	return paths.InstallerIndicatorFile + string(v.UID)
}

// GetPVSubPath returns the subpath in the local data PV.
// We use the UID so that we create unique paths in the PV.  If the PV is reused
// for a new vdb, the UID will be different.
func (v *VerticaDB) GetPVSubPath(subPath string) string {
	return fmt.Sprintf("%s/%s", v.UID, subPath)
}

// GetDBDataPath get the data path for the current database
func (v *VerticaDB) GetDBDataPath() string {
	return fmt.Sprintf("%s/%s", v.Spec.Local.DataPath, v.Spec.DBName)
}

// GetCatalogPath gets the catalog path for the current database
func (v *VerticaDB) GetDBCatalogPath() string {
	return fmt.Sprintf("%s/%s", v.Spec.Local.GetCatalogPath(), v.Spec.DBName)
}

// GetDBDepotPath gets the depot path for the current database
func (v *VerticaDB) GetDBDepotPath() string {
	return fmt.Sprintf("%s/%s", v.Spec.Local.DepotPath, v.Spec.DBName)
}

// GetCommunalPath returns the path to use for communal storage
func (v *VerticaDB) GetCommunalPath() string {
	// We include the UID in the communal path to generate a unique path for
	// each new instance of vdb. This means we can't use the same base path for
	// different databases and we don't require any cleanup if the vdb was
	// recreated.
	if !v.Spec.Communal.IncludeUIDInPath {
		return v.Spec.Communal.Path
	}
	return fmt.Sprintf("%s/%s", v.Spec.Communal.Path, v.UID)
}

const (
	PrimarySubclusterType   = "primary"
	SecondarySubclusterType = "secondary"
)

// GetType returns the type of the subcluster in string form
func (s *Subcluster) GetType() string {
	if s.IsPrimary {
		return PrimarySubclusterType
	}
	return SecondarySubclusterType
}

// GenCompatibleFQDN returns a name of the subcluster that is
// compatible inside a fully-qualified domain name.
func (s *Subcluster) GenCompatibleFQDN() string {
	m := regexp.MustCompile(`_`)
	return m.ReplaceAllString(s.Name, "-")
}

// GetServiceName returns the name of the service object that route traffic to
// this subcluster.
func (s *Subcluster) GetServiceName() string {
	if s.ServiceName == "" {
		return s.GenCompatibleFQDN()
	}
	return s.ServiceName
}

// FindSubclusterForServiceName will find any subclusters that match the given service name
func (v *VerticaDB) FindSubclusterForServiceName(svcName string) (scs []*Subcluster, totalSize int32) {
	totalSize = int32(0)
	scs = []*Subcluster{}
	for i := range v.Spec.Subclusters {
		if v.Spec.Subclusters[i].GetServiceName() == svcName {
			scs = append(scs, &v.Spec.Subclusters[i])
			totalSize += v.Spec.Subclusters[i].Size
		}
	}
	return scs, totalSize
}

// RequiresTransientSubcluster checks if an online upgrade requires a
// transient subcluster.  A transient subcluster exists if the template is
// filled out.
func (v *VerticaDB) RequiresTransientSubcluster() bool {
	return v.Spec.TemporarySubclusterRouting != nil &&
		v.Spec.TemporarySubclusterRouting.Template.Name != "" &&
		v.Spec.TemporarySubclusterRouting.Template.Size > 0
}

// IsOnlineUpgradeInProgress returns true if an online upgrade is in progress
func (v *VerticaDB) IsOnlineUpgradeInProgress() bool {
	return v.isConditionIndexSet(OnlineUpgradeInProgressIndex)
}

// IsConditionSet will return true if the status condition is set to true.
// If the condition is not in the array then this implies the condition is
// false.
func (v *VerticaDB) IsConditionSet(statusCondition VerticaDBConditionType) (bool, error) {
	inx, ok := VerticaDBConditionIndexMap[statusCondition]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", statusCondition)
	}
	return v.isConditionIndexSet(inx), nil
}

// isConditionIndexSet will check a status condition when the index is already
// known.  If the array isn't sized yet for the index then we assume the
// condition is off.
func (v *VerticaDB) isConditionIndexSet(inx int) bool {
	// A missing condition implies false
	return inx < len(v.Status.Conditions) && v.Status.Conditions[inx].Status == corev1.ConditionTrue
}

// GetUpgradeRequeueTime returns default upgrade requeue time if not set in the CRD
func (v *VerticaDB) GetUpgradeRequeueTime() time.Duration {
	if v.Spec.UpgradeRequeueTime == 0 {
		return time.Second * time.Duration(URTime)
	}
	return time.Second * time.Duration(v.Spec.UpgradeRequeueTime)
}

// buildTransientSubcluster creates a temporary read-only sc based on an existing subcluster
func (v *VerticaDB) BuildTransientSubcluster(imageOverride string) *Subcluster {
	return &Subcluster{
		Name:              v.Spec.TemporarySubclusterRouting.Template.Name,
		Size:              v.Spec.TemporarySubclusterRouting.Template.Size,
		IsTransient:       true,
		ImageOverride:     imageOverride,
		IsPrimary:         false,
		NodeSelector:      v.Spec.TemporarySubclusterRouting.Template.NodeSelector,
		Affinity:          v.Spec.TemporarySubclusterRouting.Template.Affinity,
		PriorityClassName: v.Spec.TemporarySubclusterRouting.Template.PriorityClassName,
		Tolerations:       v.Spec.TemporarySubclusterRouting.Template.Tolerations,
		Resources:         v.Spec.TemporarySubclusterRouting.Template.Resources,
		// We ignore any parameter that is specific to the subclusters service
		// object.  These are ignored since transient don't have their own
		// service objects.
	}
}

// FindSubclusterStatus will find a SubclusterStatus entry for the given
// subcluster name.  Returns false if none can be found.
func (v *VerticaDB) FindSubclusterStatus(scName string) (SubclusterStatus, bool) {
	for i := range v.Status.Subclusters {
		if v.Status.Subclusters[i].Name == scName {
			return v.Status.Subclusters[i], true
		}
	}
	return SubclusterStatus{}, false
}

// IsHTTPServerDisabled explicitly checks if the http server is disabled. If set
// to auto or enabled, this returns false.
func (v *VerticaDB) IsHTTPServerDisabled() bool {
	return v.Spec.DeprecatedHTTPServerMode == HTTPServerModeDisabled
}

// IsHTTPServerEnabled will return true if the http server is enabled to run for
// this instance of the vdb.
func (v *VerticaDB) IsHTTPServerEnabled() bool {
	if v.IsHTTPServerDisabled() {
		return false
	}
	if v.Spec.DeprecatedHTTPServerMode == HTTPServerModeEnabled {
		return true
	}
	// For auto (or an empty string), we only use the http server if we are on a
	// vertica version that supports it.
	inf, ok := v.MakeVersionInfo()
	// We cannot make any inference about the version, so assume https server
	// isn't enabled
	if !ok {
		return false
	}
	return inf.IsEqualOrNewer(HTTPServerAutoMinVersion)
}

// IsHTTPServerAuto returns true if http server is auto.
func (v *VerticaDB) IsHTTPServerAuto() bool {
	return v.Spec.DeprecatedHTTPServerMode == HTTPServerModeAuto ||
		v.Spec.DeprecatedHTTPServerMode == ""
}

// IsEON returns true if the instance is an EON database. Officially, all
// deployments of this CR will result in an EON database. However, as a backdoor
// for developers, if you set the shardCount to 0, we will create an enterprise
// database. The webhook enforces ShardCount > 0, so that part needs to be
// overridden to take affect.
func (v *VerticaDB) IsEON() bool {
	return v.Spec.ShardCount > 0
}

// IsAdditionalConfigMapEmpty returns true if there is no extra
// config parameters.
func (v *VerticaDB) IsAdditionalConfigMapEmpty() bool {
	return len(v.Spec.Communal.AdditionalConfig) == 0
}

// IsDepotVolumeEmptyDir returns true if the depot volume's type
// is emptyDir.
func (v *VerticaDB) IsDepotVolumeEmptyDir() bool {
	return v.Spec.Local.DepotVolume == EmptyDir
}

// IsDepotVolumePersistentVolume returns true if the depot volume's type
// is persistentVolume.
func (v *VerticaDB) IsDepotVolumePersistentVolume() bool {
	return v.Spec.Local.DepotVolume == PersistentVolume ||
		v.Spec.Local.DepotVolume == ""
}

// IsknownDepotVolumeType returns true if the depot volume's type is
// a valid one.
func (v *VerticaDB) IsKnownDepotVolumeType() bool {
	if v.IsDepotVolumeEmptyDir() || v.IsDepotVolumePersistentVolume() {
		return true
	}
	return false
}

// getFirstPrimarySubcluster returns the first primary subcluster defined in the vdb
func (v *VerticaDB) GetFirstPrimarySubcluster() *Subcluster {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.IsPrimary {
			return sc
		}
	}
	// We should never get here because the webhook prevents a vdb with no primary.
	return nil
}

// GetIgnoreClusterLease will check if the cluster lease should be ignored.
func (v *VerticaDB) GetIgnoreClusterLease() bool {
	return vmeta.IgnoreClusterLease(v.Annotations)
}

// SetIgnoreClusterLease will set the annotation to ignore the cluster lease
func (v *VerticaDB) SetIgnoreClusterLease(val bool) {
	v.Annotations[vmeta.IgnoreClusterLeaseAnnotation] = strconv.FormatBool(val)
}

// GetIgnoreUpgradePath will check if the upgrade path can be ignored
func (v *VerticaDB) GetIgnoreUpgradePath() bool {
	return vmeta.IgnoreUpgradePath(v.Annotations)
}

// SetIgnoreUpgradePath will set the annotation to ignore the upgrade path
func (v *VerticaDB) SetIgnoreUpgradePath(val bool) {
	v.Annotations[vmeta.IgnoreUpgradePathAnnotation] = strconv.FormatBool(val)
}

// GetRestartTimeout returns the timeout value for restart node and start db
func (v *VerticaDB) GetRestartTimeout() int {
	return vmeta.GetRestartTimeout(v.Annotations)
}
