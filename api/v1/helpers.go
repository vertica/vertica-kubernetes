/*
Copyright [2021-2024] Open Text.

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
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultS3Region       = "us-east-1"
	DefaultGCloudRegion   = "US-EAST1"
	DefaultGCloudEndpoint = "https://storage.googleapis.com"

	// Additional server config parameters
	S3SseKmsKeyID = "S3SseKmsKeyId"

	RFC1123DNSSubdomainNameRegex = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	RFC1035DNSLabelNameRegex     = `^[a-z]([a-z0-9\-]{0,61}[a-z0-9])?$`

	MainCluster = ""

	VerticaDBNameKey = "verticaDBName"
	SandboxNameKey   = "sandboxName"
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

// GenerateOwnerReference creates an owner reference for the current VerticaDB
func (v *VerticaDB) GenerateOwnerReference() metav1.OwnerReference {
	isController := true
	blockOwnerDeletion := false
	return metav1.OwnerReference{
		APIVersion:         GroupVersion.String(),
		Kind:               VerticaDBKind,
		Name:               v.Name,
		UID:                v.GetUID(),
		Controller:         &isController,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}

// FindTransientSubcluster will return a pointer to the transient subcluster if one exists
func (v *VerticaDB) FindTransientSubcluster() *Subcluster {
	for i := range v.Spec.Subclusters {
		if v.Spec.Subclusters[i].IsTransient() {
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
			Name:      nm.Name,
			Namespace: nm.Namespace,
			UID:       "abcdef-ghi",
			Annotations: map[string]string{
				vmeta.VClusterOpsAnnotation: vmeta.VClusterOpsAnnotationFalse,
				vmeta.VersionAnnotation:     "v23.4.0",
			},
		},
		Spec: VerticaDBSpec{
			AutoRestartVertica: true,
			Labels:             make(map[string]string),
			Annotations:        make(map[string]string),
			Image:              "vertica-k8s:latest",
			InitPolicy:         CommunalInitPolicyCreate,
			Communal: CommunalStorage{
				Path:             "s3://nimbusdb/cchen",
				Endpoint:         "http://minio",
				CredentialSecret: "s3-auth",
				AdditionalConfig: make(map[string]string),
			},
			Local: LocalStorage{
				DataPath:    "/data",
				DepotPath:   "/depot",
				DepotVolume: PersistentVolume,
				RequestSize: resource.MustParse("10Gi"),
			},
			DBName:     "db",
			ShardCount: 12,
			Subclusters: []Subcluster{
				{
					Name:        "defaultsubcluster",
					Size:        3,
					ServiceType: corev1.ServiceTypeClusterIP,
					Type:        PrimarySubcluster,
					Proxy:       Proxy{Image: "vertica-client-proxy:latest"},
				},
			},
		},
	}
}

// MakeVDBForHTTP is a helper that constructs a VerticaDB struct with http enabled.
// This is intended for test purposes.
func MakeVDBForHTTP(httpServerTLSSecretName string) *VerticaDB {
	vdb := MakeVDB()
	vdb.Annotations[vmeta.VersionAnnotation] = HTTPServerMinVersion
	vdb.Spec.NMATLSSecret = httpServerTLSSecretName
	return vdb
}

// MakeVDBForVclusterOps is a helper that constructs a VerticaDB struct for
// vclusterops. This is intended for test purposes.
func MakeVDBForVclusterOps() *VerticaDB {
	vdb := MakeVDB()
	vdb.Annotations[vmeta.VersionAnnotation] = VcluseropsAsDefaultDeploymentMethodMinVersion
	vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
	return vdb
}

// MakeVDBForScrutinize is a helper that constructs a VerticaDB struct for
// scrutinize. This is intended for test purposes.
func MakeVDBForScrutinize() *VerticaDB {
	vdb := MakeVDBForVclusterOps()
	vdb.Annotations[vmeta.VersionAnnotation] = ScrutinizeDBPasswdInSecretMinVersion
	return vdb
}

// GenSubclusterMap will organize all of the subclusters into a map for quicker lookup.
// The key is the subcluster name and the value is a pointer to its Subcluster struct.
func (v *VerticaDB) GenSubclusterMap() map[string]*Subcluster {
	scMap := map[string]*Subcluster{}
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		scMap[sc.Name] = sc
	}
	return scMap
}

// GenSandboxMap will build a map that can find a sandbox by name.
func (v *VerticaDB) GenSandboxMap() map[string]*Sandbox {
	sbMap := map[string]*Sandbox{}
	for i := range v.Spec.Sandboxes {
		sb := &v.Spec.Sandboxes[i]
		sbMap[sb.Name] = sb
	}
	return sbMap
}

// findSubclusterIndexInSandbox will return the index of the suclusterName in sandbox.
// when the sandboxName is not found in the sandbox, -1 will be returned
func (v *VerticaDB) findSubclusterIndexInSandbox(targetSclusterName string, sandbox *Sandbox) int {
	for i, subclusterName := range sandbox.Subclusters {
		if subclusterName.Name == targetSclusterName {
			return i
		}
	}
	return -1
}

// GenSubclusterSandboxMap will scan all sandboxes and return a map
// with subcluster name as the key and sandbox name as the value
func (v *VerticaDB) GenSubclusterSandboxMap() map[string]string {
	scSbMap := make(map[string]string)
	for i := range v.Spec.Sandboxes {
		sb := &v.Spec.Sandboxes[i]
		for _, sc := range sb.Subclusters {
			scSbMap[sc.Name] = sb.Name
		}
	}
	return scSbMap
}

// GenSubclusterSandboxStatusMap will scan sandbox status and return a map
// with subcluster name as the key and sandbox name as the value
func (v *VerticaDB) GenSubclusterSandboxStatusMap() map[string]string {
	scSbMap := make(map[string]string)
	for i := range v.Status.Sandboxes {
		sb := &v.Status.Sandboxes[i]
		for _, sc := range sb.Subclusters {
			scSbMap[sc] = sb.Name
		}
	}
	return scSbMap
}

// GenStatusSandboxMap() returns a map from status. The key is sandbox name and value is the sandbox pointer
func (v *VerticaDB) GenStatusSandboxMap() map[string]*SandboxStatus {
	statusSboxMap := make(map[string]*SandboxStatus)
	for i := range v.Status.Sandboxes {
		sBox := &v.Status.Sandboxes[i]
		statusSboxMap[sBox.Name] = sBox
	}
	return statusSboxMap
}

// GenStatusSubclusterMap() returns a map from status. The key is subcluster name and value is the subcluster pointer
func (v *VerticaDB) GenStatusSubclusterMap() map[string]*SubclusterStatus {
	statusSclusterMap := make(map[string]*SubclusterStatus)
	for i := range v.Status.Subclusters {
		sCluster := &v.Status.Subclusters[i]
		statusSclusterMap[sCluster.Name] = sCluster
	}
	return statusSclusterMap
}

// GenStatusSClusterIndexMap will organize all of the subclusters into a map so we
// can quickly find its index in the status.subclusters[] array.
func (v *VerticaDB) GenStatusSClusterIndexMap() map[string]int {
	m := make(map[string]int)
	for i := range v.Status.Subclusters {
		m[v.Status.Subclusters[i].Name] = i
	}
	return m
}

// GenSandboxSubclusterMapForUnsandbox will compare sandbox status and spec
// for finding subclusters that need to be unsandboxed, this function returns a map
// with sandbox name as the key and its subclusters (need to be unsandboxed) as the value
func (v *VerticaDB) GenSandboxSubclusterMapForUnsandbox() map[string][]string {
	unsandboxSbScMap := make(map[string][]string)
	vdbScSbMap := v.GenSubclusterSandboxMap()
	statusScSbMap := v.GenSubclusterSandboxStatusMap()
	for sc, sbInStatus := range statusScSbMap {
		sbInVdb, found := vdbScSbMap[sc]
		// if a subcluster is removed or put into another sandbox in spec.sandboxes,
		// we need to unsandbox the subcluster
		if !found || sbInVdb != sbInStatus {
			unsandboxSbScMap[sbInStatus] = append(unsandboxSbScMap[sbInStatus], sc)
		}
	}
	return unsandboxSbScMap
}

// GenSubclusterIndexMap will organize all of the subclusters into a map so we
// can quickly find its index in the spec.subclusters[] array.
func (v *VerticaDB) GenSubclusterIndexMap() map[string]int {
	m := make(map[string]int)
	for i := range v.Spec.Subclusters {
		m[v.Spec.Subclusters[i].Name] = i
	}
	return m
}

// GenSandboxIndexMap will create a map that allows us to figure out the index
// in vdb.Spec.Sandboxes for each sandbox. Returns a map of sandbox name to its
// index position.
func (v *VerticaDB) GenSandboxIndexMap() map[string]int {
	m := make(map[string]int)
	for i := range v.Spec.Sandboxes {
		m[v.Spec.Sandboxes[i].Name] = i
	}
	return m
}

func isValidRFC1123DNSSubdomainName(name string) bool {
	if len(name) < 1 || len(name) > 253 {
		return false
	}
	r := regexp.MustCompile(RFC1123DNSSubdomainNameRegex)
	return r.MatchString(name)
}

func isValidRFC1035DNSLabelName(name string) bool {
	r := regexp.MustCompile(RFC1035DNSLabelNameRegex)
	return r.MatchString(name)
}

// IsValidSubclusterName validates the subcluster name is valid.  We have rules
// about its name because it is included in the name of the statefulset, so we
// must adhere to the Kubernetes rules for object names.
func IsValidSubclusterName(scName string) bool {
	return isValidRFC1123DNSSubdomainName(scName)
}

func IsValidServiceName(svcName string) bool {
	return isValidRFC1035DNSLabelName(svcName)
}

// MakeCondition create and initialize a new metav1.Condition
func MakeCondition(ctype string, status metav1.ConditionStatus, reason string) *metav1.Condition {
	r := reason
	if r == "" {
		r = UnknownReason
	}
	return &metav1.Condition{
		Type:   ctype,
		Status: status,
		Reason: r,
	}
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
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(v.Spec.Local.DataPath, "/"), v.Spec.DBName)
}

// GetCatalogPath gets the catalog path for the current database
func (v *VerticaDB) GetDBCatalogPath() string {
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(v.Spec.Local.GetCatalogPath(), "/"), v.Spec.DBName)
}

// GetDBDepotPath gets the depot path for the current database
func (v *VerticaDB) GetDBDepotPath() string {
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(v.Spec.Local.DepotPath, "/"), v.Spec.DBName)
}

// GetCommunalPath returns the path to use for communal storage
func (v *VerticaDB) GetCommunalPath() string {
	// We include the UID in the communal path to generate a unique path for
	// each new instance of vdb. This means we can't use the same base path for
	// different databases and we don't require any cleanup if the vdb was
	// recreated.
	if !v.IncludeUIDInPath() {
		return v.Spec.Communal.Path
	}
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(v.Spec.Communal.Path, "/"), v.UID)
}

// GenCompatibleFQDN returns a name of the subcluster that is
// compatible inside a fully-qualified domain name.
func (s *Subcluster) GenCompatibleFQDN() string {
	return GenCompatibleFQDNHelper(s.Name)
}

func GenCompatibleFQDNHelper(name string) string {
	m := regexp.MustCompile(`_`)
	return m.ReplaceAllString(name, "-")
}

// GetStatefulSetName returns the name of the statefulset for this subcluster
func (s *Subcluster) GetStatefulSetName(vdb *VerticaDB) string {
	stsOverrideName := vmeta.GetStsNameOverride(s.Annotations)
	if stsOverrideName != "" {
		return stsOverrideName
	}
	return fmt.Sprintf("%s-%s", vdb.Name, s.GenCompatibleFQDN())
}

// GetServiceName returns the name of the service object that route traffic to
// this subcluster.
func (s *Subcluster) GetServiceName() string {
	if s.ServiceName == "" {
		return s.GenCompatibleFQDN()
	}
	return s.ServiceName
}

// GetService gets the external service associated with this subcluster
func (s *Subcluster) GetService(ctx context.Context, vdb *VerticaDB, c client.Client) (svc corev1.Service, err error) {
	name := types.NamespacedName{
		Name:      vdb.Name + "-" + s.GetServiceName(),
		Namespace: vdb.GetNamespace(),
	}
	if err := c.Get(ctx, name, &svc); err != nil {
		return corev1.Service{}, err
	}
	return
}

// IsZombie checks if a subcluster is zombie. A zombie subcluster
// is one that is no longer in vdb spec, no longer part of a sandbox
// but still has a sandbox label different from the main cluster on
// its statefulset.
// It can happen when you remove a subcluster from spec.subclusters
// and spec.sandboxes at once
func (s *Subcluster) IsZombie(vdb *VerticaDB) bool {
	sbName := s.Annotations[vmeta.SandboxNameLabel]
	if sbName == MainCluster {
		return false
	}
	scInSandboxMap := vdb.GenSubclusterSandboxMap()
	scInSandboxStatusMap := vdb.GenSubclusterSandboxStatusMap()
	_, foundInSandbox := scInSandboxMap[s.Name]
	_, foundInSandboxStatus := scInSandboxStatusMap[s.Name]
	return !foundInSandbox && !foundInSandboxStatus
}

// GetStsSize returns the number of replicas that will be assigned
// to the statefulset. By default it is the subcluster's size, and
// zero if the subcluster has shutdown true.
func (s *Subcluster) GetStsSize(vdb *VerticaDB) int32 {
	if !s.Shutdown {
		return s.Size
	}
	scStatusMap := vdb.GenSubclusterStatusMap()
	ss := scStatusMap[s.Name]
	if ss != nil && ss.Shutdown {
		return 0
	}
	return s.Size
}

// GetVProxyConfigMapName returns the name of the client proxy config map
func (s *Subcluster) GetVProxyConfigMapName(vdb *VerticaDB) string {
	return fmt.Sprintf("%s-%s-proxy-cm", vdb.Name, s.Name)
}

// GetVProxyDeploymentName returns the name of the client proxy deployment
func (s *Subcluster) GetVProxyDeploymentName(vdb *VerticaDB) string {
	return fmt.Sprintf("%s-%s-proxy", vdb.Name, s.Name)
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

// GetTransientSubclusterName returns the name of the transient subcluster, if
// it should exist. The bool output parameter will be false if no transient is
// used.
func (v *VerticaDB) GetTransientSubclusterName() (string, bool) {
	if !v.RequiresTransientSubcluster() {
		return "", false
	}
	return v.Spec.TemporarySubclusterRouting.Template.Name, true
}

// IsOnlineUpgradeInProgress returns true if an online upgrade is in progress
func (v *VerticaDB) IsOnlineUpgradeInProgress() bool {
	return v.IsStatusConditionTrue(OnlineUpgradeInProgress)
}

// IsROOnlineUpgradeInProgress returns true if an read-only online upgrade is in progress
func (v *VerticaDB) IsROUpgradeInProgress() bool {
	return v.IsStatusConditionTrue(ReadOnlyOnlineUpgradeInProgress)
}

// IsUpgradeInProgress returns true if an upgrade is in progress
func (v *VerticaDB) IsUpgradeInProgress() bool {
	return v.IsStatusConditionTrue(UpgradeInProgress)
}

// IsStatusConditionTrue returns true when the conditionType is present and set to
// `metav1.ConditionTrue`
func (v *VerticaDB) IsStatusConditionTrue(statusCondition string) bool {
	return meta.IsStatusConditionTrue(v.Status.Conditions, statusCondition)
}

// IsStatusConditionFalse returns true when the conditionType is present and set to
// `metav1.ConditionFalse`
func (v *VerticaDB) IsStatusConditionFalse(statusCondition string) bool {
	return meta.IsStatusConditionFalse(v.Status.Conditions, statusCondition)
}

// FindStatusCondition finds the conditionType in conditions.
func (v *VerticaDB) FindStatusCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(v.Status.Conditions, conditionType)
}

// IsSandBoxUpgradeInProgress returns true if is an upgrade
// is already occurring in the given sandbox
func (v *VerticaDB) IsSandBoxUpgradeInProgress(sbName string) bool {
	sb := v.GetSandboxStatus(sbName)
	return sb != nil && sb.UpgradeState.UpgradeInProgress
}

func (v *VerticaDB) GetUpgradeStatus(sbName string) (string, error) {
	if sbName == MainCluster {
		return v.Status.UpgradeStatus, nil
	}
	sb, err := v.GetSandboxStatusCheck(sbName)
	if err != nil {
		return "", err
	}
	return sb.UpgradeState.UpgradeStatus, nil
}

// buildTransientSubcluster creates a temporary read-only sc based on an existing subcluster
func (v *VerticaDB) BuildTransientSubcluster(imageOverride string) *Subcluster {
	return &Subcluster{
		Name:              v.Spec.TemporarySubclusterRouting.Template.Name,
		Size:              v.Spec.TemporarySubclusterRouting.Template.Size,
		ImageOverride:     imageOverride,
		Type:              TransientSubcluster,
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

// GetFirstPrimarySubcluster returns the first primary subcluster defined in the vdb
func (v *VerticaDB) GetFirstPrimarySubcluster() *Subcluster {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.IsPrimary() {
			return sc
		}
	}
	// We should never get here because the webhook prevents a vdb with no primary.
	return nil
}

// HasSecondarySubclusters returns true if at least 1 secondary subcluster
// exists in the database.
func (v *VerticaDB) HasSecondarySubclusters() bool {
	for i := range v.Spec.Subclusters {
		if v.Spec.Subclusters[i].IsSecondary() {
			return true
		}
	}
	return false
}

// IsAutoUpgradePolicy returns true
func (v *VerticaDB) IsAutoUpgradePolicy() bool {
	return v.Spec.UpgradePolicy == "" || v.Spec.UpgradePolicy == AutoUpgrade
}

// GetUpgradePolicyToUse returns the upgrade policy that the db should use.
// It will take into account the settings in the vdb as well as what is
// supported in the server. This will never return the auto upgrade policy. If
// you need the current value of that field, just refer to it by referencing
// Spec.UpgradePolicy.
func (v *VerticaDB) GetUpgradePolicyToUse() UpgradePolicyType {
	if v.Spec.UpgradePolicy == OfflineUpgrade {
		return OfflineUpgrade
	}

	if v.IsAutoUpgradePolicy() && v.IsKSafety0() {
		return OfflineUpgrade
	}

	// If we cannot get the version, always default to offline. We cannot make
	// any assumptions about what upgrade policy the server supports.
	vinf, ok := v.MakeVersionInfo()
	if !ok {
		return OfflineUpgrade
	}

	// The Online option can only be chosen explicitly. Although eventually,
	// the Auto option will automatically select this method, we first need to
	// complete the implementation of this new policy.
	if v.Spec.UpgradePolicy == OnlineUpgrade {
		// Online upgrade requires that we scale out the cluster. See if
		// there is evidence that we have already scaled past 3 nodes (CE
		// license limit), or we have a license defined.
		const ceLicenseLimit = 3
		if v.isOnlineUpgradeSupported(vinf) &&
			!v.IsKSafety0() &&
			(v.getNumberOfNodes() > ceLicenseLimit || v.Spec.LicenseSecret != "") &&
			// online upgrade is not allowed if there is already a sandbox
			// in vertica, except from the one used for online upgrade
			!v.containsSandboxNotForUpgrade() {
			return OnlineUpgrade
		} else if vinf.IsEqualOrNewer(ReadOnlyOnlineUpgradeVersion) {
			return ReadOnlyOnlineUpgrade
		}
	}

	if (v.Spec.UpgradePolicy == ReadOnlyOnlineUpgrade || v.IsAutoUpgradePolicy()) &&
		vinf.IsEqualOrNewer(ReadOnlyOnlineUpgradeVersion) {
		return ReadOnlyOnlineUpgrade
	}

	return OfflineUpgrade
}

// containsSandboxNotForUpgrade returns true if there is already a sandbox in the database, except
// from the one created for online upgrade.
func (v *VerticaDB) containsSandboxNotForUpgrade() bool {
	if len(v.Status.Sandboxes) > 1 || len(v.Spec.Sandboxes) > 1 {
		return true
	}
	upgradeSandbox := vmeta.GetOnlineUpgradeSandbox(v.Annotations)
	if len(v.Status.Sandboxes) == 1 {
		if upgradeSandbox != v.Status.Sandboxes[0].Name {
			return true
		}
	}
	if len(v.Spec.Sandboxes) == 1 {
		return upgradeSandbox != v.Spec.Sandboxes[0].Name
	}
	return false
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

// GetCreateDBNodeStartTimeout returns the timeout value for createdb node startup
func (v *VerticaDB) GetCreateDBNodeStartTimeout() int {
	return vmeta.GetCreateDBNodeStartTimeout(v.Annotations)
}

// IsNMASideCarDeploymentEnabled returns true if the conditions to run NMA
// in a sidecar are met
func (v *VerticaDB) IsNMASideCarDeploymentEnabled() bool {
	if !vmeta.UseVClusterOps(v.Annotations) {
		return false
	}
	vinf, hasVersion := v.MakeVersionInfo()
	// Assume NMA is running as a sidecar if version isn't present. We rely on
	// the operator to correct things later if it turns out we are running an
	// older release that doesn't have support.
	if !hasVersion {
		return true
	}
	return vinf.IsEqualOrNewer(NMAInSideCarDeploymentMinVersion)
}

// IsMonolithicDeploymentEnabled returns true if NMA must run in the
// same container as vertica
func (v *VerticaDB) IsMonolithicDeploymentEnabled() bool {
	if !vmeta.UseVClusterOps(v.Annotations) {
		return false
	}
	return !v.IsNMASideCarDeploymentEnabled()
}

// IsKSafety0 returns true if k-safety of 0 is set.
func (v *VerticaDB) IsKSafety0() bool {
	return vmeta.IsKSafety0(v.Annotations)
}

// GetKSafety returns the string value of the k-safety value
func (v *VerticaDB) GetKSafety() string {
	if v.IsKSafety0() {
		return "0"
	}
	return "1"
}

// GetRequeueTime returns the time in seconds to wait for the next reconiliation iteration.
func (v *VerticaDB) GetRequeueTime() int {
	return vmeta.GetRequeueTime(v.Annotations)
}

// GetUpgradeRequeueTime returns the time in seconds to wait between
// reconciliations during an upgrade. This is the raw value as set in the CR.
func (v *VerticaDB) GetUpgradeRequeueTime() int {
	return vmeta.GetUpgradeRequeueTime(v.Annotations)
}

// GetUpgradeRequeueTimeDuration returns default upgrade requeue time if not set
// in the CRD. The value returned is of type Duration.
func (v *VerticaDB) GetUpgradeRequeueTimeDuration() time.Duration {
	if v.GetUpgradeRequeueTime() == 0 {
		return time.Second * time.Duration(URTime)
	}
	return time.Second * time.Duration(v.GetUpgradeRequeueTime())
}

// GetSSHSecretName returns the name of the secret that contains SSH keys to use
// for admintools style of deployments.
func (v *VerticaDB) GetSSHSecretName() string {
	return vmeta.GetSSHSecretName(v.Annotations)
}

// IncludeUIDInPath will return true if the UID should be included in the
// communal path to make it unique.
func (v *VerticaDB) IncludeUIDInPath() bool {
	return vmeta.IncludeUIDInPath(v.Annotations)
}

// IsHDFS returns true if the communal path is stored in an HDFS path
func (v *VerticaDB) IsHDFS() bool {
	for _, p := range hdfsPrefixes {
		if strings.HasPrefix(v.Spec.Communal.Path, p) {
			return true
		}
	}
	return false
}

// IsS3 returns true if VerticaDB has a communal path for S3 compatible storage.
func (v *VerticaDB) IsS3() bool {
	return strings.HasPrefix(v.Spec.Communal.Path, S3Prefix)
}

// ISGCloud returns true if VerticaDB has a communal path in Google Cloud Storage
func (v *VerticaDB) IsGCloud() bool {
	return strings.HasPrefix(v.Spec.Communal.Path, GCloudPrefix)
}

// IsAzure returns true if VerticaDB has a communal path in Azure Blob Storage
func (v *VerticaDB) IsAzure() bool {
	return strings.HasPrefix(v.Spec.Communal.Path, AzurePrefix)
}

// IsSseS3 returns true if VerticaDB is setup for S3 SSE-S3 server-side encryption
func (v *VerticaDB) IsSseS3() bool {
	return strings.EqualFold(string(v.Spec.Communal.S3ServerSideEncryption), string(SseS3))
}

// IsSseKMS returns true if VerticaDB is setup for S3 SSE-KMS server-side encryption
func (v *VerticaDB) IsSseKMS() bool {
	return strings.EqualFold(string(v.Spec.Communal.S3ServerSideEncryption), string(SseKMS))
}

// IsSseC returns true if VerticaDB is setup for S3 SSE-C server-side encryption
func (v *VerticaDB) IsSseC() bool {
	return strings.EqualFold(string(v.Spec.Communal.S3ServerSideEncryption), string(SseC))
}

// IsKnownSseType returns true if VerticaDB is setup for S3 server-side encryption
func (v *VerticaDB) IsKnownSseType() bool {
	if v.IsSseS3() || v.IsSseKMS() || v.IsSseC() {
		return true
	}
	return false
}

// IsKnownCommunalPrefix returns true if the communal has a known prefix that
// indicates the type of communal storage. False means the communal path was
// empty or is a POSIX path.
func (v *VerticaDB) IsKnownCommunalPrefix() bool {
	if v.IsHDFS() || v.IsS3() || v.IsGCloud() || v.IsAzure() {
		return true
	}
	return false
}

// HasKerberosConfig returns true if VerticaDB is setup for Kerberos authentication.
func (v *VerticaDB) HasKerberosConfig() bool {
	// We have a webhook check that makes sure if the principal is set, the
	// other things are set too.
	return v.GetKerberosServiceName() != ""
}

func (v *VerticaDB) GetKerberosRealm() string {
	return v.Spec.Communal.AdditionalConfig[vmeta.KerberosRealmConfig]
}

func (v *VerticaDB) GetKerberosServiceName() string {
	return v.Spec.Communal.AdditionalConfig[vmeta.KerberosServiceNameConfig]
}

func (s *Subcluster) IsPrimary() bool {
	return s.Type == PrimarySubcluster || s.Type == SandboxPrimarySubcluster
}

func (s *Subcluster) IsMainPrimary() bool {
	return s.Type == PrimarySubcluster
}

func (s *Subcluster) IsSandboxPrimary() bool {
	return s.Type == SandboxPrimarySubcluster
}

func (s *Subcluster) IsSecondary() bool {
	return s.Type == SecondarySubcluster
}

func (s *Subcluster) IsTransient() bool {
	return s.Type == TransientSubcluster
}

// GetType returns the type of the subcluster in string form
func (s *Subcluster) GetType() string {
	// Transient subclusters are considered secondary subclusters. This exists
	// for historical reasons because we added separate labels for
	// primary/secondary and transient.
	if s.IsTransient() {
		return SecondarySubcluster
	}
	return s.Type
}

func (v *VerticaDBStatus) InstallCount() int32 {
	var c int32
	for i := range v.Subclusters {
		c += v.Subclusters[i].InstallCount()
	}
	return c
}

func (s *SubclusterStatus) InstallCount() int32 {
	var c int32
	for i := range s.Detail {
		if s.Detail[i].Installed {
			c++
		}
	}
	return c
}

// GetVerticaUser returns the name of Vertica superuser generated in database creation.
func (v *VerticaDB) GetVerticaUser() string {
	return vmeta.GetSuperuserName(v.Annotations)
}

// GetEncryptSpreadComm will return "vertica" if encryptSpreadComm is set to
// an empty string, otherwise return the value of encryptSpreadComm
func (v *VerticaDB) GetEncryptSpreadComm() string {
	if v.Spec.EncryptSpreadComm == "" {
		return EncryptSpreadCommWithVertica
	}
	return v.Spec.EncryptSpreadComm
}

func (v *VerticaDB) IsKSafetyCheckStrict() bool {
	return vmeta.IsKSafetyCheckStrict(v.Annotations)
}

// IsValidRestorePointPolicy returns true if the RestorePointPolicy is properly specified,
// i.e., it has a non-empty archive, and either a valid index or a valid id (but not both).
func (r *RestorePointPolicy) IsValidRestorePointPolicy() bool {
	return r != nil && r.Archive != "" && ((r.Index > 0) != (r.ID != ""))
}

// IsValidForSaveRestorePoint returns true if archive name to be used
// for creating a restore point is set.
func (r *RestorePointPolicy) IsValidForSaveRestorePoint() bool {
	return r != nil && r.Archive != ""
}

// IsRestoreDuringReviveEnabled will return whether the vdb is configured to initialize by reviving from
// a restore point in an archive
func (v *VerticaDB) IsRestoreDuringReviveEnabled() bool {
	return v.Spec.InitPolicy == CommunalInitPolicyRevive && v.Spec.RestorePoint != nil
}

// IsSaveRestorepointEnabled returns true if the status condition that
// control restore point is set to true.
func (v *VerticaDB) IsSaveRestorepointEnabled() bool {
	return v.IsStatusConditionTrue(SaveRestorePointNeeded)
}

// IsHTTPSTLSConfGenerationEnabled return true if the httpstls.json file should
// be generated by the installer.
func (v *VerticaDB) IsHTTPSTLSConfGenerationEnabled() (bool, error) {
	// Early-out if the annotaton is set.
	if vmeta.IsHTTPSTLSConfGenerationAnnotationSet(v.Annotations) {
		enabled := vmeta.IsHTTPSTLSConfGenerationEnabled(v.Annotations)
		return enabled, nil
	}

	// The httpstls.json file doesn't need to be created for databases that were
	// created in 24.1.0+. These versions will automatically seed the catalog
	// with a HTTPS cert during bootstrap-catalog. The next few checks determine
	// if that applies. If any of the checks fail, we will return true to
	// indicate we must generate httpstls.json.
	//
	// 1. Only consider if the operator is going to create the database. That's
	// the only way we know what version we're using during bootstrap-catalog.
	if v.Spec.InitPolicy != CommunalInitPolicyCreate &&
		v.Spec.InitPolicy != CommunalInitPolicyCreateSkipPackageInstall {
		return true, nil
	}
	// 2. If we have created the database already, we don't know what version it
	// was created in. So, we just assume the generation is needed. For cases
	// where we don't want to generate it after creating the database, we rely
	// on the operator setting an annotation to prevent us from generating in
	// subsequent reconcile iterations.
	ok := v.IsStatusConditionTrue(DBInitialized)
	if ok {
		return true, nil
	}
	// 3. Are we at the minimum version that has the bootstrap-catalog change to
	// generate the cert? Older versions return true, newer versions return false.
	inf, err := v.MakeVersionInfoCheck()
	if err != nil {
		return false, err
	}
	return !inf.IsEqualOrNewer(AutoGenerateHTTPSCertsForNewDatabasesMinVersion), nil
}

// GenSubclusterStatusMap returns a map that has a subcluster name as key
// and its status as value
func (v *VerticaDB) GenSubclusterStatusMap() map[string]*SubclusterStatus {
	scMap := map[string]*SubclusterStatus{}
	for i := range v.Status.Subclusters {
		sc := &v.Status.Subclusters[i]
		scMap[sc.Name] = sc
	}
	return scMap
}

func (v *VerticaDB) GetSubclusterSandboxName(scName string) string {
	for i := range v.Status.Sandboxes {
		for j := range v.Status.Sandboxes[i].Subclusters {
			if scName == v.Status.Sandboxes[i].Subclusters[j] {
				return v.Status.Sandboxes[i].Name
			}
		}
	}
	return MainCluster
}

// getNumberOfNodes returns the number of nodes defined in the database, as per the CR.
func (v *VerticaDB) getNumberOfNodes() int {
	count := 0
	for i := range v.Spec.Subclusters {
		count += int(v.Spec.Subclusters[i].Size)
	}
	return count
}

// GetSandbox returns the sandbox given by name. A nil pointer is returned if
// not found.
func (v *VerticaDB) GetSandbox(sbName string) *Sandbox {
	for i := range v.Spec.Sandboxes {
		if v.Spec.Sandboxes[i].Name == sbName {
			return &v.Spec.Sandboxes[i]
		}
	}
	return nil
}

// GetSandboxStatus returns the status of the sandbox given by name. A nil pointer is returned if
// not found.
func (v *VerticaDB) GetSandboxStatus(sbName string) *SandboxStatus {
	for i := range v.Status.Sandboxes {
		if v.Status.Sandboxes[i].Name == sbName {
			return &v.Status.Sandboxes[i]
		}
	}
	return nil
}

// GetSandboxStatusCheck is like GetSandboxStatus but returns an error if the sandbox
// is missing in the status. Use this in places where it is a failure if the sandbox
// is not in the status
func (v *VerticaDB) GetSandboxStatusCheck(sbName string) (*SandboxStatus, error) {
	sb := v.GetSandboxStatus(sbName)
	if sb == nil {
		return nil, fmt.Errorf("could not find sandbox %q in status", sbName)
	}
	return sb, nil
}

// IsSubclusterInStatus will check if a subcluster in vdb status
func (v *VerticaDB) IsSubclusterInStatus(scName string) bool {
	for i := range v.Status.Subclusters {
		if v.Status.Subclusters[i].Name == scName {
			return true
		}
	}
	return false
}

// GetSubclustersForReplicaGroup returns the names of the subclusters that are part of a replica group.
func (v *VerticaDB) GetSubclustersForReplicaGroup(groupName string) []string {
	scNames := []string{}
	for i := range v.Spec.Subclusters {
		if g, found := v.Spec.Subclusters[i].Annotations[vmeta.ReplicaGroupAnnotation]; found && g == groupName {
			scNames = append(scNames, v.Spec.Subclusters[i].Name)
		}
	}
	return scNames
}
