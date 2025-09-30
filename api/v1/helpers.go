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
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultS3Region       = "us-east-1"
	DefaultS3Endpoint     = "https://s3.amazonaws.com"
	DefaultGCloudRegion   = "US-EAST1"
	DefaultGCloudEndpoint = "https://storage.googleapis.com"

	// Additional server config parameters
	S3SseKmsKeyID = "S3SseKmsKeyId"

	RFC1123DNSSubdomainNameRegex = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	RFC1035DNSLabelNameRegex     = `^[a-z]([a-z0-9\-]{0,61}[a-z0-9])?$`

	MainCluster = ""

	VerticaDBNameKey = "verticaDBName"
	SandboxNameKey   = "sandboxName"
	invalidNameChars = "$=<>`" + `'^\".@*?#&/:;{}()[] \~!%+|,`

	// TLS modes
	tlsModeDisable           = "disable"
	tlsModeEnable            = "enable"
	tlsModeVerifyCA          = "verify_ca"
	tlsModeTryVerify         = "try_verify"
	tlsModeVerifyFull        = "verify_full"
	nmaTLSModeDisable        = "disable"
	nmaTLSModeEnable         = "enable"
	nmaTLSModeVerifyCA       = "verify-ca"
	DefaultServiceHTTPSPort  = 8443
	DefaultServiceClientPort = 5433

	// Deployment methods
	DeploymentMethodAT = "admintools"
	DeploymentMethodVC = "vclusterops"
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

func SetVDBForTLS(v *VerticaDB) {
	v.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
	v.Annotations[vmeta.VersionAnnotation] = TLSAuthMinVersion
	v.Annotations[vmeta.VClusterOpsAnnotation] = trueString
}

func SetVDBWithHTTPSTLSConfigSet(v *VerticaDB, secretName string) {
	SetVDBForTLS(v)
	v.Status.TLSConfigs = []TLSConfigStatus{
		{
			Name:   HTTPSNMATLSConfigName,
			Secret: secretName,
			Mode:   tlsModeTryVerify,
		},
	}
}

// MakeVDB is a helper that constructs a fully formed VerticaDB struct using the sample name.
// This is intended for test purposes.
func MakeVDB() *VerticaDB {
	nm := MakeVDBName()
	replicas := int32(1)
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
				vmeta.VClusterOpsAnnotation:   vmeta.VClusterOpsAnnotationFalse,
				vmeta.VersionAnnotation:       "v23.4.0",
				vmeta.EnableTLSAuthAnnotation: trueString,
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
					Annotations: make(map[string]string),
					Size:        3,
					ServiceType: corev1.ServiceTypeClusterIP,
					Type:        PrimarySubcluster,
					Proxy: &ProxySubclusterConfig{
						Replicas: &replicas,
					},
				},
			},
			Proxy: &Proxy{
				Image: "opentext/client-proxy:latest",
			},
			ServiceHTTPSPort:  DefaultServiceHTTPSPort,
			ServiceClientPort: DefaultServiceClientPort,
			HTTPSNMATLS:       &TLSConfigSpec{},
			ClientServerTLS:   &TLSConfigSpec{},
		},
	}
}

// MakeVDBForTLS is a helper that constructs a VerticaDB struct with TLS enabled.
func MakeVDBForTLS() *VerticaDB {
	vdb := MakeVDB()
	SetVDBForTLS(vdb)
	return vdb
}

// MakeVDBForHTTP is a helper that constructs a VerticaDB struct with http enabled.
// This is intended for test purposes.
func MakeVDBForHTTP(httpServerTLSSecretName string) *VerticaDB {
	vdb := MakeVDB()
	vdb.Annotations[vmeta.VersionAnnotation] = HTTPServerMinVersion
	vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = vmeta.AnnotationTrue
	vdb.Spec.HTTPSNMATLS.Secret = httpServerTLSSecretName
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

// MakeVDBForCertRotationEnabled is a helper that constructs a VerticaDB struct for
// cert rotation. This is intended for test purposes.
func MakeVDBForCertRotationEnabled() *VerticaDB {
	vdb := MakeVDB()
	SetVDBForTLS(vdb)
	return vdb
}

func MakeTLSWithAutoRotate(secrets []string, interval int, secret string) *TLSConfigSpec {
	return &TLSConfigSpec{
		Secret: secret,
		AutoRotate: &TLSAutoRotate{
			Secrets:  secrets,
			Interval: interval,
		},
	}
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

// findSubclusterIndexInSandbox will return the index of the targetSclusterName in sandbox.
// when the targetSclusterName is not found in the sandbox, -1 will be returned
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

// GenSandboxSubclusterTypeMap will scan all sandboxes and return a map
// with subcluster name as the key and sandbox subcluster type as the value
func (v *VerticaDB) GenSandboxSubclusterTypeMap() map[string]string {
	scSbMap := make(map[string]string)
	for i := range v.Spec.Sandboxes {
		sb := &v.Spec.Sandboxes[i]
		for _, sc := range sb.Subclusters {
			scSbMap[sc.Name] = sc.Type
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

func MakeTLSConfig(name, secret, mode string) *TLSConfigStatus {
	return &TLSConfigStatus{
		Name:   name,
		Secret: secret,
		Mode:   mode,
	}
}

func MakeClientServerTLSConfig(secret, mode string) *TLSConfigStatus {
	return MakeTLSConfig(ClientServerTLSConfigName, secret, mode)
}

func MakeHTTPSNMATLSConfig(secret, mode string) *TLSConfigStatus {
	return MakeTLSConfig(HTTPSNMATLSConfigName, secret, mode)
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
	// when preserving the db directory, we need to use a fixed path
	if vmeta.GetPreserveDBDirectory(v.Annotations) {
		return fmt.Sprintf("%s/%s", "preserved-db-directory", subPath)
	}
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

// IsSubclusterInSandbox returns true if the given subcluster is in the
// sandbox status
func (s *SandboxStatus) IsSubclusterInSandbox(scName string) bool {
	for i := range s.Subclusters {
		if scName == s.Subclusters[i] {
			return true
		}
	}
	return false
}

// convertSubclusterType converts both sandbox and main-cluster subcluster types to
// main-cluster cluster type
func convertSubclusterType(ctype string) string {
	if ctype == PrimarySubcluster {
		return PrimarySubcluster
	}
	return SecondarySubcluster
}

// IsSubclusterOpNeeded returns true if all subclusters in spec.Subclusters are not the same
// as subclusters in status.subclusters
func (v *VerticaDB) IsSubclusterOpNeeded() bool {
	type subcluster struct {
		Size     int32
		Type     string
		Shutdown bool
	}
	specScs := make(map[string]subcluster)
	for i := range v.Spec.Subclusters {
		specScs[v.Spec.Subclusters[i].Name] = subcluster{
			Size:     v.Spec.Subclusters[i].Size,
			Type:     convertSubclusterType(v.Spec.Subclusters[i].Type),
			Shutdown: v.Spec.Subclusters[i].Shutdown,
		}
	}
	statusScs := make(map[string]subcluster)
	for i := range v.Status.Subclusters {
		statusScs[v.Status.Subclusters[i].Name] = subcluster{
			Size:     v.Status.Subclusters[i].UpNodeCount,
			Type:     convertSubclusterType(v.Status.Subclusters[i].Type),
			Shutdown: v.Status.Subclusters[i].Shutdown,
		}
	}
	return !reflect.DeepEqual(specScs, statusScs)
}

// IsSandboxOpNeeded returns true if all subclusters in spec.Sandbox are not the same
// as subclusters in status.sandbox
func (v *VerticaDB) IsSandboxOpNeeded() bool {
	specScSbMap := v.GenSubclusterSandboxMap()
	statusScSbMap := v.GenSubclusterSandboxStatusMap()
	return !reflect.DeepEqual(specScSbMap, statusScSbMap)
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

func (s *Subcluster) GetVProxyDeploymentName(vdb *VerticaDB) string {
	depOverrideName := vmeta.GetVPDepNameOverride(s.Annotations)
	if depOverrideName != "" {
		return depOverrideName
	}
	return fmt.Sprintf("%s-%s-proxy", vdb.Name, GenCompatibleFQDNHelper(s.Name))
}

func (s *Subcluster) GetVProxyConfigMapName(vdb *VerticaDB) string {
	return GetVProxyConfigMapName(s.GetVProxyDeploymentName(vdb))
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
func GetVProxyConfigMapName(prefix string) string {
	return fmt.Sprintf("%s-cm", prefix)
}

// GetVProxyDeploymentName returns the name of the client proxy deployment
func (v *VerticaDB) GetVProxyDeploymentName(scName string) string {
	return fmt.Sprintf("%s-%s-proxy", v.Name, GenCompatibleFQDNHelper(scName))
}

// FindSubclusterForServiceName will find any subclusters that match the given service name.
// If service name is empty, it will return all the subclusters in vdb.
func (v *VerticaDB) FindSubclusterForServiceName(svcName string) (scs []*Subcluster, totalSize int32) {
	totalSize = int32(0)
	scs = []*Subcluster{}
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if svcName == "" || sc.GetServiceName() == svcName {
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

func (v *VerticaDB) IsTLSConfigUpdateInProgress() bool {
	return v.IsStatusConditionTrue(TLSConfigUpdateInProgress)
}

func (v *VerticaDB) IsTLSCertRollbackNeeded() bool {
	return v.IsStatusConditionTrue(TLSCertRollbackNeeded)
}

func (v *VerticaDB) IsTLSCertRollbackInProgress() bool {
	return v.IsStatusConditionTrue(TLSCertRollbackInProgress)
}

func (v *VerticaDB) FindTLSCertRollbackNeededCondition() *metav1.Condition {
	return v.FindStatusCondition(TLSCertRollbackNeeded)
}

func (v *VerticaDB) IsTLSCertRollbackEnabled() bool {
	return vmeta.IsEnableTLSRollbackAnnotationSet(v.Annotations)
}

// GetTLSCertRollbackReason returns the reason or the point
// which cert rotation failed in. This is used to know the ops
// needed to rollback
func (v *VerticaDB) GetTLSCertRollbackReason() string {
	cond := v.FindTLSCertRollbackNeededCondition()
	if cond == nil {
		return ""
	}

	return cond.Reason
}

// IsHTTPSRollbackFailureBeforeCertHealthPolling returns true if https cert rotation failed
// without altering the current tls config
func (v *VerticaDB) IsHTTPSRollbackFailureBeforeCertHealthPolling() bool {
	return v.GetTLSCertRollbackReason() == FailureBeforeHTTPSCertHealthPollingReason
}

// IsHTTPSRollbackFailureAfterCertHealthPolling returns true if https cert rotation failed
// after altering the current tls config
func (v *VerticaDB) IsHTTPSRollbackFailureAfterCertHealthPolling() bool {
	return v.GetTLSCertRollbackReason() == RollbackAfterHTTPSCertRotationReason
}

// IsRollbackAfterServerCertRotation returns true if client-server cert rotation failed
// (but tls config will not be changed)
func (v *VerticaDB) IsRollbackAfterServerCertRotation() bool {
	return v.GetTLSCertRollbackReason() == RollbackAfterServerCertRotationReason
}

// IsRollbackAfterNMACertRotation returns true if NMA cert rotation failed
// but tls config changed
func (v *VerticaDB) IsRollbackAfterNMACertRotation() bool {
	return v.GetTLSCertRollbackReason() == RollbackAfterNMACertRotationReason
}

// GetTLSConfigSpecByName returns the TLSConfigSpec object for a certain tlsconfig (clientServer or httpsNMA)
func (v *VerticaDB) GetTLSConfigSpecByName(tlsConfig string) *TLSConfigSpec {
	if tlsConfig == ClientServerTLSConfigName {
		return v.Spec.ClientServerTLS
	}
	return v.Spec.HTTPSNMATLS
}

// IsAutoCertRotationEnabled checks if automatic cert rotation is enabled for
// for a certain tlsconfig (clientServer or httpsNMA). This could mean set in
// the spec or spec has been unset but the status is still set.
func (v *VerticaDB) IsAutoCertRotationEnabled(tlsConfig string) bool {
	if !vmeta.UseTLSAuth(v.Annotations) {
		return false
	}
	config := v.GetTLSConfigSpecByName(tlsConfig)
	specSet := config != nil && config.AutoRotate != nil && len(config.AutoRotate.Secrets) > 0
	statusSet := len(v.GetAutoRotateSecrets(tlsConfig)) > 0
	return specSet || statusSet
}

// GetAutoRotateSecrets gets the list of auto-rotate secrets from status
// for a certain tlsconfig (clientServer or httpsNMA)
func (v *VerticaDB) GetAutoRotateSecrets(tlsConfig string) []string {
	config := v.GetTLSConfigByName(tlsConfig)
	if config == nil {
		return []string{}
	}
	return config.AutoRotateSecrets
}

// GetTLSLastUpdate gets the last update time from the status
// for a certain tlsconfig (clientServer or httpsNMA)
func (v *VerticaDB) GetTLSLastUpdate(tlsConfig string) metav1.Time {
	config := v.GetTLSConfigByName(tlsConfig)
	if config == nil {
		return metav1.Time{}
	}
	return config.LastUpdate
}

// GetTLSNextUpdate gets the next update time from the status
// for a certain tlsconfig (clientServer or httpsNMA). It does so
// using LastUpdate from status and autoRotate.interval from spec.
func (v *VerticaDB) GetTLSNextUpdate(tlsConfig string) *metav1.Time {
	status := v.GetTLSConfigByName(tlsConfig)
	if status == nil || status.LastUpdate.IsZero() {
		return nil
	}

	interval := v.GetTLSConfigAutoRotate(tlsConfig).Interval
	next := status.LastUpdate.Time.Add(time.Duration(interval) * time.Minute)
	return &metav1.Time{Time: next}
}

// GetTLSConfigAutoRotate gets the TLSAutoRotate from spec
// for a certain tlsconfig (clientServer or httpsNMA)
func (v *VerticaDB) GetTLSConfigAutoRotate(tlsConfig string) *TLSAutoRotate {
	config := v.GetTLSConfigSpecByName(tlsConfig)
	if config == nil {
		return nil
	}
	return config.AutoRotate
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

// IsDepotVolumeManaged returns true if vertica.com/disable-depot-volume-management
// is false or not set
func (v *VerticaDB) IsDepotVolumeManaged() bool {
	return !vmeta.DisableDepotVolumeManagement(v.Annotations)
}

// GetFirstPrimarySubcluster returns the first primary subcluster defined in the vdb
func (v *VerticaDB) GetFirstPrimarySubcluster() *Subcluster {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.IsPrimary(v) {
			return sc
		}
	}
	// We should never get here because the webhook prevents a vdb with no primary.
	return nil
}

// GetPromaryCount returns the number of primary nodes in the cluster.
func (v *VerticaDB) GetPrimaryCount() int {
	sizeSum := 0
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.IsPrimary(v) && !sc.IsSandboxPrimary(v) {
			sizeSum += int(sc.Size)
		}
	}
	return sizeSum
}

// GetMainPrimaryUpCount returns the number of primary nodes in the main subcluster in up state.
func (v *VerticaDB) GetMainPrimaryUpCount() int {
	sizeSum := 0
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.IsPrimary(v) && !sc.IsSandboxPrimary(v) {
			ss, ok := v.FindSubclusterStatus(sc.Name)
			if ok {
				sizeSum += int(ss.UpNodeCount)
			}
		}
	}
	return sizeSum
}

// GetSandboxPrimaryUpCount returns the number of primary nodes in a sandbox in up state.
func (v *VerticaDB) GetSandboxPrimaryUpCount(sbName string) int {
	if sbName == MainCluster {
		return v.GetMainPrimaryUpCount()
	}

	sbMap := v.GenSandboxMap()
	sb, ok := sbMap[sbName]
	if !ok {
		return 0
	}

	sizeSum := 0
	for i := range sb.Subclusters {
		sbSc := sb.Subclusters[i]
		ss, ok := v.FindSubclusterStatus(sbSc.Name)
		if ok && ss.Type == SandboxPrimarySubcluster {
			sizeSum += int(ss.UpNodeCount)
		}
	}
	return sizeSum
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

// GetActiveConnectionsDrainSeconds returns time in seconds to wait for a subcluster/database users' disconnection
func (v *VerticaDB) GetActiveConnectionsDrainSeconds() int {
	return vmeta.GetActiveConnectionsDrainSeconds(v.Annotations)
}

// IsHTTPProbeSupported returns true if the version supports certs
func (v *VerticaDB) IsHTTPProbeSupported(ver string) bool {
	vinf, hasVersion := v.MakeVersionInfo()
	if ver != "" {
		vinf, hasVersion = v.GetVersion(ver)
	}
	// Assume we are running a version that does not support cert rotation
	// if version is not present.
	if !hasVersion {
		return false
	}
	return vinf.IsEqualOrNewer(TLSAuthMinVersion)
}

// IsNMASideCarDeploymentEnabled returns true if the conditions to run NMA
// in a sidecar are met
func (v *VerticaDB) IsNMASideCarDeploymentEnabled() bool {
	if !v.UseVClusterOpsDeployment() {
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
	if !v.UseVClusterOpsDeployment() {
		return false
	}
	return !v.IsNMASideCarDeploymentEnabled()
}

// ShouldEnableHTTPS returns true if the deployment method is vclusterOps
// and the version supports it.
func (v *VerticaDB) ShouldEnableHTTPS() bool {
	if !v.UseVClusterOpsDeployment() {
		return false
	}
	vinf, hasVersion := v.MakeVersionInfo()
	// When version isn't present but vclusterOps annotation is set to true,
	// we assume the version supports vcusterOps.
	if !hasVersion {
		return true
	}
	return vinf.IsEqualOrNewer(VcluseropsAsDefaultDeploymentMethodMinVersion)
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

// HasKSafetyAfterRemoval checks whether a main cluster or sandbox is k-safety
// after some primary nodes removed
func (v *VerticaDB) HasKSafetyAfterRemoval(sbName string, offset int) bool {
	minHosts := KSafety0MinHosts
	if !v.IsKSafety0() {
		minHosts = KSafety1MinHosts
	}

	primaryCount := 0
	if sbName == MainCluster {
		primaryCount = v.GetMainPrimaryUpCount()
	} else {
		primaryCount = v.GetSandboxPrimaryUpCount(sbName)
	}

	return primaryCount-offset >= minHosts
}

// GetRequeueTime returns the time in seconds to wait for the next reconciliation iteration.
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

// IsPathHDFS returns true if the path is an HDFS path
func (v *VerticaDB) IsPathHDFS(path string) bool {
	for _, p := range hdfsPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// IsHDFS returns true if the communal path is stored in an HDFS path
func (v *VerticaDB) IsHDFS() bool {
	return v.IsPathHDFS(v.Spec.Communal.Path)
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

func (s *Subcluster) IsPrimary(v *VerticaDB) bool {
	return s.Type == PrimarySubcluster || s.IsSandboxPrimary(v)
}

// HasAdditionalBuckets returns true if additionalBuckets is configured for data replication
func (v *VerticaDB) HasAdditionalBuckets() bool {
	return len(v.Spec.AdditionalBuckets) != 0
}

// GetBucket returns the bucket name from the path URL
func GetBucket(path string) string {
	re := regexp.MustCompile(`([a-z]\d+)://(.*)`)
	m := re.FindAllStringSubmatch(path, 1)

	if len(m) == 0 || len(m[0]) < 3 {
		return path
	}

	p := strings.Split(m[0][2], "/")
	if len(p) == 0 || len(p[0]) < 3 {
		return m[0][2]
	}

	return strings.TrimRight(p[0], "/")
}

func (s *Subcluster) IsMainPrimary() bool {
	return s.Type == PrimarySubcluster
}

func (s *Subcluster) IsSandboxPrimary(v *VerticaDB) bool {
	return v.GetSandboxSubclusterType(s.Name) == PrimarySubcluster
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
	if s.IsTransient() || s.Type == "" {
		return SecondarySubcluster
	}

	return s.Type
}

// GetSubcluster returns the subcluster based on the subcluster name
func (v *VerticaDB) GetSubcluster(scName string) *Subcluster {
	scMap := v.GenSubclusterMap()
	if sc, ok := scMap[scName]; ok {
		return sc
	}
	return nil
}

// GetSubclusterType calls GetType but returns the type based on its sandbox type
func (s *Subcluster) GetSubclusterType(v *VerticaDB) string {
	if s.IsSandboxPrimary(v) {
		return SandboxPrimarySubcluster
	}

	return s.GetType()
}

// GetTypeByName returns the type of the subcluster by its name
func (spec *VerticaDBSpec) GetTypeByName(scName string) string {
	for i := range spec.Subclusters {
		if spec.Subclusters[i].Name == scName {
			return spec.Subclusters[i].Type
		}
	}

	// return empty if sc does not exist
	return ""
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

// GetPasswordSecret returns the password secret
func (v *VerticaDB) GetPasswordSecret() string {
	// status holds the current password
	if v.Status.PasswordSecret != nil {
		return *v.Status.PasswordSecret
	}
	// Spec holds the desired password
	return v.Spec.PasswordSecret
}

// GetPasswordSecretForSandbox returns the password secret for a sandbox.
// It will return the main cluster secret if the sandbox does not have its own secret.
func (v *VerticaDB) GetPasswordSecretForSandbox(sbName string) (secret string) {
	if sbName == MainCluster {
		return v.GetPasswordSecret()
	}

	sandbox := v.GetSandboxStatus(sbName)
	if sandbox != nil && sandbox.PasswordSecret != nil {
		return *sandbox.PasswordSecret
	}
	return v.GetPasswordSecret()
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

func (v *VerticaDB) IsFetchNodeDetailsLogDisabled() bool {
	return vmeta.IsFetchNodeDetailsLogDisabled(v.Annotations)
}

func (v *VerticaDB) GetCacheDuration() uint64 {
	duration := vmeta.GetCacheDuration(v.Annotations)
	if duration < 0 {
		return 0
	}
	return uint64(duration)
}

func (v *VerticaDB) ShouldRemoveTLSSecret() bool {
	return vmeta.ShouldRemoveTLSSecret(v.Annotations)
}

// GetHTTPSPollingMaxRetries returns the max number of retries for HTTPS polling during cert rotation.
func (v *VerticaDB) GetHTTPSPollingMaxRetries() int {
	return vmeta.GetHTTPSPollingMaxRetries(v.Annotations)
}

// GetHTTPSPollingCurrentRetries returns current retry for HTTPS polling or 0 if not set.
func (v *VerticaDB) GetHTTPSPollingCurrentRetries() int {
	return v.Status.HTTPSPollingCurrentRetry
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

// IsHTTPSConfigEnabled returns true if tls is enabled and https tls config
// exists in the db. It means the db ops can start using tls
func (v *VerticaDB) IsHTTPSConfigEnabled() bool {
	return v.IsSetForTLS() &&
		v.GetHTTPSNMATLSSecretInUse() != ""
}

// IsHTTPSConfigEnabledWithCreate returns true if tls is enabled and https tls config
// exists in the db. It means the db ops can start using tls. For revive, there is know way to know
// the db had tls configs until after revive so can't make any assumptions
func (v *VerticaDB) IsHTTPSConfigEnabledWithCreate() bool {
	if v.Spec.InitPolicy == CommunalInitPolicyCreate {
		return v.IsHTTPSConfigEnabled()
	}

	return v.IsSetForTLS()
}

// IsClientServerConfigEnabled returns true if tls is enabled and client-server tls config
// exists in the db
func (v *VerticaDB) IsClientServerConfigEnabled() bool {
	return v.IsSetForTLS() &&
		v.GetClientServerTLSSecretInUse() != ""
}

// IsSetForTLS returns true if VerticaDB is set and ready for tls.
// It does not mean vclusterops can now operate using tls, for
// that we need to wait until tls configurations are created
func (v *VerticaDB) IsSetForTLS() bool {
	return v.IsValidVersionForTLS(TLSAuthMinVersion) &&
		vmeta.UseTLSAuth(v.Annotations)
}

func (v *VerticaDB) IsSetForTLSVersionAndCipher() bool {
	return v.IsValidVersionForTLS(TLSVersionCipherMinVersion) &&
		vmeta.UseTLSAuth(v.Annotations)
}

// IsValidVersionForTLS returns true if the server version
// supports tls
func (v *VerticaDB) IsValidVersionForTLS(minVersion string) bool {
	if !v.UseVClusterOpsDeployment() {
		return false
	}
	vinf, hasVersion := v.MakeVersionInfo()
	// Assume we are running a version that does not support cert rotation
	// if version is not present.
	if !hasVersion {
		return false
	}

	return vinf.IsEqualOrNewer(minVersion)
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

// GetSandboxSubclusterType returns the subcluster type in a sandbox
func (v *VerticaDB) GetSandboxSubclusterType(scName string) string {
	typeScSbMap := v.GenSandboxSubclusterTypeMap()
	return typeScSbMap[scName]
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

// GetSubclusterStatusType returns the subcluster status type
func (v *VerticaDB) GetSubclusterStatusType(scName string) string {
	scStatus, ok := v.FindSubclusterStatus(scName)
	if ok {
		return scStatus.Type
	}

	return ""
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

// GetSubclustersInSandbox returns the subclusters in the given sandbox
func (v *VerticaDB) GetSubclustersInSandbox(sbName string) []string {
	scNames := []string{}
	scSbMap := v.GenSubclusterSandboxMap()
	if sbName == MainCluster {
		for i := range v.Spec.Subclusters {
			scName := v.Spec.Subclusters[i].Name
			if _, found := scSbMap[scName]; !found {
				scNames = append(scNames, scName)
			}
		}
		return scNames
	}
	sb := v.GetSandbox(sbName)
	if sb == nil {
		return nil
	}
	for i := range sb.Subclusters {
		scNames = append(scNames, sb.Subclusters[i].Name)
	}
	return scNames
}

// UseVClusterDeployment returns true if the deployment method is vclusterOps
func (v *VerticaDB) UseVClusterOpsDeployment() bool {
	if v.Status.DeploymentMethod == DeploymentMethodVC {
		return true
	} else if v.Status.DeploymentMethod == DeploymentMethodAT {
		return false
	}

	// when deploymentMethod is empty in status, check annotation
	return vmeta.UseVClusterOps(v.Annotations)
}

// GetHPAMetrics extract an return hpa metrics from MetricDefinition struct.
func (v *VerticaAutoscaler) GetHPAMetrics() []autoscalingv2.MetricSpec {
	metrics := make([]autoscalingv2.MetricSpec, len(v.Spec.CustomAutoscaler.Hpa.Metrics))
	for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
		metrics[i] = v.Spec.CustomAutoscaler.Hpa.Metrics[i].Metric
	}
	return metrics
}

// ValidatePrometheusAuthBasic will check if required key exists for type PrometheusAuthBasic
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthBasic(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyUsername]; !ok {
		return errors.New("username not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyPassword]; !ok {
		return errors.New("password not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthBearer will check if required key exists for type PrometheusAuthBearer
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthBearer(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyBearerToken]; !ok {
		return errors.New("bearerToken not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthTLS will check if required key exists for type PrometheusAuthTLS
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthTLS(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyCa]; !ok {
		return errors.New("ca not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyCert]; !ok {
		return errors.New("cert not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyKey]; !ok {
		return errors.New("key not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthCustom will check if required key exists for type PrometheusAuthCustom
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthCustom(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyCustomAuthHeader]; !ok {
		return errors.New("customAuthHeader not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyCustomAuthValue]; !ok {
		return errors.New("customAuthValue not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthTLSAndBasic will check if required key exists for type PrometheusAuthTLSAndBasic
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthTLSAndBasic(secretData map[string][]byte) error {
	if err := authmode.ValidatePrometheusAuthBasic(secretData); err != nil {
		return err
	}
	if err := authmode.ValidatePrometheusAuthTLS(secretData); err != nil {
		return err
	}
	return nil
}

// GetMap Convert PrometheusSpec to map[string]string
func (p *PrometheusSpec) GetMap() map[string]string {
	result := make(map[string]string)
	result["serverAddress"] = p.ServerAddress
	result["query"] = p.Query
	result["threshold"] = fmt.Sprintf("%d", p.Threshold)
	// Only add ScaleInThreshold if it is non-zero
	if p.ScaleInThreshold != 0 {
		result["activationThreshold"] = fmt.Sprintf("%d", p.ScaleInThreshold)
	}

	return result
}

// GetMap converts CPUMemorySpec to map[string]string
func (r *CPUMemorySpec) GetMap() map[string]string {
	result := make(map[string]string)
	result["value"] = fmt.Sprintf("%d", r.Threshold)
	return result
}

// GetMetadata returns the metric parameters map
func (s *ScaleTrigger) GetMetadata() map[string]string {
	if s.IsPrometheusMetric() {
		return s.Prometheus.GetMap()
	}
	return s.Resource.GetMap()
}

func (s *ScaleTrigger) IsNil() bool {
	return s.Prometheus == nil && s.Resource == nil
}

func (s *ScaleTrigger) IsPrometheusMetric() bool {
	return s.Type == PrometheusTriggerType || s.Type == ""
}

func (s *ScaleTrigger) GetUnsafeSslStr() string {
	return strconv.FormatBool(s.Prometheus.UnsafeSsl)
}

func (s *ScaleTrigger) GetType() string {
	if s.Type == "" {
		return string(PrometheusTriggerType)
	}
	return string(s.Type)
}

// MakeScaledObjectSpec builds a sample scaleObjectSpec.
// This is intended for test purposes.
func MakeScaledObjectSpec() *ScaledObjectSpec {
	return &ScaledObjectSpec{
		MinReplicas:     &[]int32{3}[0],
		MaxReplicas:     &[]int32{6}[0],
		PollingInterval: &[]int32{5}[0],
		Metrics: []ScaleTrigger{
			{
				Name: "sample-metric",
				Prometheus: &PrometheusSpec{
					ServerAddress: "http://localhost",
					Query:         "query",
					Threshold:     5,
				},
			},
		},
	}
}

// HasScaleInThreshold returns true if scale in threshold is set
func (v *VerticaAutoscaler) HasScaleInThreshold() bool {
	if !v.IsHpaEnabled() {
		return false
	}
	for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
		m := &v.Spec.CustomAutoscaler.Hpa.Metrics[i]
		if m.ScaleInThreshold != nil {
			return true
		}
	}
	return false
}

// GetMinReplicas calculates the minReplicas based on the scale in
// threshold, and returns it
func (v *VerticaAutoscaler) GetMinReplicas() *int32 {
	vasCopy := v.DeepCopy()
	if v.HasScaleInThreshold() {
		return &vasCopy.Spec.TargetSize
	}
	return vasCopy.Spec.CustomAutoscaler.Hpa.MinReplicas
}

// GetMetricMap returns a map whose key is the metric name and the value is
// the metric's definition.
func (v *VerticaAutoscaler) GetMetricMap() map[string]*MetricDefinition {
	mMap := make(map[string]*MetricDefinition)
	for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
		m := &v.Spec.CustomAutoscaler.Hpa.Metrics[i]
		var name string
		if m.Metric.Pods != nil {
			name = m.Metric.Pods.Metric.Name
		} else if m.Metric.Object != nil {
			name = m.Metric.Object.Metric.Name
		} else if m.Metric.External != nil {
			name = m.Metric.External.Metric.Name
		} else if m.Metric.Resource != nil {
			name = m.Metric.Resource.Name.String()
		} else {
			name = m.Metric.ContainerResource.Name.String()
		}
		mMap[name] = m
	}
	return mMap
}

// GetMetricTarget returns the autoscalingv2 metric target
func GetMetricTarget(metric *autoscalingv2.MetricSpec) *autoscalingv2.MetricTarget {
	if metric == nil {
		return nil
	}
	switch metric.Type {
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods != nil {
			return &metric.Pods.Target
		}
	case autoscalingv2.ObjectMetricSourceType:
		if metric.Object != nil {
			return &metric.Object.Target
		}
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External != nil {
			return &metric.External.Target
		}
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil {
			return &metric.Resource.Target
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource != nil {
			return &metric.ContainerResource.Target
		}
	}
	return nil
}

func (v *VerticaDB) GetTLSConfigByName(name string) *TLSConfigStatus {
	return FindTLSConfig(v.Status.TLSConfigs, "Name", name)
}

func (v *VerticaDB) GetTLSConfigBySecret(secret string) *TLSConfigStatus {
	return FindTLSConfig(v.Status.TLSConfigs, "Secret", secret)
}

func (v *VerticaDB) GetTLSConfigByMode(mode string) *TLSConfigStatus {
	return FindTLSConfig(v.Status.TLSConfigs, "Mode", mode)
}

func (v *VerticaDB) GetSecretInUse(name string) string {
	if v.GetTLSConfigByName(name) == nil {
		return ""
	}
	return v.GetTLSConfigByName(name).Secret
}

func (v *VerticaDB) GetHTTPSNMATLSSecretInUse() string {
	return v.GetSecretInUse(HTTPSNMATLSConfigName)
}

// GetNonEmptyHTTPSNMATLSSecret returns the httpsNMA secret
// from the status if non empty or from the spec
func (v *VerticaDB) GetNonEmptyHTTPSNMATLSSecret() string {
	if v.GetHTTPSNMATLSSecretInUse() != "" {
		return v.GetHTTPSNMATLSSecretInUse()
	}

	return v.GetHTTPSNMATLSSecret()
}

func (v *VerticaDB) GetClientServerTLSSecretInUse() string {
	return v.GetSecretInUse(ClientServerTLSConfigName)
}

// GetValueForTLSConfigMap determines which value (spec or status) should be written to the NMA TLS ConfigMap.
// The decision is made per certificate type (https or clientServer) to avoid prematurely updating NMA
// with a new cert that hasn’t been rotated yet.
//
// Rules:
//  1. If statusValue is empty (e.g., during initial create), use specValue.
//  2. If a rollback is in progress, use statusValue to keep NMA pointing at the last known good cert.
//  3. If this cert’s rotation is in progress (started but not yet marked finished), use specValue
//     so NMA can start using the new cert.
//  4. Otherwise, default to statusValue so we don’t break NMA communication by using an unready cert.
//
// This ensures that when rotating multiple certs in the same iteration, each configmap update
// only changes the fields for the cert currently being rotated.
func (v *VerticaDB) GetValueForTLSConfigMap(specValue, statusValue, tlsConfigName string) string {
	if statusValue == "" {
		return specValue
	}

	if v.IsTLSCertRollbackNeeded() {
		return statusValue
	}

	// Only switch to spec if this cert’s rotation is in progress
	updateNotFinished := v.IsStatusConditionTrue(HTTPSTLSConfigUpdateFinished)
	if tlsConfigName == ClientServerTLSConfigName {
		updateNotFinished = v.IsStatusConditionTrue(ClientServerTLSConfigUpdateFinished)
	}

	if updateNotFinished {
		return specValue // rotation started, not done → point to new secret
	}

	return statusValue // rotation not started → keep old in-use secret
}

// NoClientServerRotationNeeded returns true if the ClientServer TLS configuration
// does not require any further rotation, meaning both the desired TLS mode
// and secret match the currently in-use values.
func (v *VerticaDB) NoClientServerRotationNeeded() bool {
	modeUpToDate := v.GetClientServerTLSMode() == v.GetClientServerTLSModeInUse()
	secretUnchanged := v.GetClientServerTLSSecret() == v.GetClientServerTLSSecretInUse()

	return modeUpToDate && secretUnchanged
}

// GetHTTPSNMATLSSecretForConfigMap returns the correct TLS secret name
// to include in the NMA configmap. It prioritizes the currently in-use
// secret if an update is still in progress or a rollback is needed.
func (v *VerticaDB) GetHTTPSNMATLSSecretForConfigMap() string {
	if !vmeta.UseTLSAuth(v.Annotations) {
		return v.GetNMATLSSecret()
	}
	return v.GetValueForTLSConfigMap(v.GetHTTPSNMATLSSecret(), v.GetHTTPSNMATLSSecretInUse(), HTTPSNMATLSConfigName)
}

// GetClientServerTLSModeForConfigMap returns the correct TLS mode
// to include in the NMA configmap. It prioritizes the currently in-use
// mode if an update is still in progress or a rollback is needed.
func (v *VerticaDB) GetClientServerTLSModeForConfigMap() string {
	return v.GetValueForTLSConfigMap(v.GetClientServerTLSMode(), v.GetClientServerTLSModeInUse(), ClientServerTLSConfigName)
}

// GetClientServerTLSSecretForConfigMap returns the correct TLS secret name
// to include in the NMA configmap. It prioritizes the currently in-use
// secret if an update is still in progress or a rollback is needed.
func (v *VerticaDB) GetClientServerTLSSecretForConfigMap() string {
	return v.GetValueForTLSConfigMap(v.GetClientServerTLSSecret(), v.GetClientServerTLSSecretInUse(), ClientServerTLSConfigName)
}

// IsCertNeededForClientServerAuth returns true if certificate is needed for client-server authentication
func (v *VerticaDB) IsCertNeededForClientServerAuth() bool {
	tlsMode := v.GetClientServerTLSMode()
	return tlsMode != tlsModeDisable && tlsMode != tlsModeEnable
}

// GetExpectedCertCommonName returns the expected common name for the TLS certificate.
// For httpsNMATLS, this is the DB admin. For clientServerTLS, it will also default to
// DB admin, but it can be overridden using clientServerTLS.commonName.
func (v *VerticaDB) GetExpectedCertCommonName(configName string) string {
	if configName == ClientServerTLSConfigName && v.Spec.ClientServerTLS != nil && v.Spec.ClientServerTLS.CommonName != "" {
		return v.Spec.ClientServerTLS.CommonName
	}
	return v.GetVerticaUser()
}

// GetNMAClientServerTLSMode returns the tlsMode for NMA client-server communication
func (v *VerticaDB) GetNMAClientServerTLSMode() string {
	tlsMode := v.GetClientServerTLSModeForConfigMap()
	switch tlsMode {
	case tlsModeDisable:
		return nmaTLSModeDisable
	case tlsModeEnable, tlsModeTryVerify:
		return nmaTLSModeEnable
	case tlsModeVerifyCA, tlsModeVerifyFull:
		// There is still a flaw in vclusterOps: create_db set_tls will fail
		// since nma cannot verify server certificate. After we extract set_tls
		// from create_db, we can remove the db init check.
		if !v.IsDBInitialized() {
			return nmaTLSModeEnable
		}
		return nmaTLSModeVerifyCA
	default:
		return nmaTLSModeEnable
	}
}

// Searches for a TLSConfig where a specified field equals a specified value
// For example, where Name=ClientServer
// Returns a pointer to the TLSConfig, or nil if not found.
func FindTLSConfig(configs []TLSConfigStatus, configField, value string) *TLSConfigStatus {
	for i := range configs {
		switch configField {
		case "Name":
			if configs[i].Name == value {
				return &configs[i]
			}
		case "Secret":
			if configs[i].Secret == value {
				return &configs[i]
			}
		case "Mode":
			if configs[i].Mode == value {
				return &configs[i]
			}
		}
	}
	return nil
}

func (v *VerticaDB) GetTLSModeInUse(name string) string {
	if v.GetTLSConfigByName(name) == nil {
		return ""
	}
	return v.GetTLSConfigByName(name).Mode
}

func (v *VerticaDB) GetHTTPSTLSModeInUse() string {
	return strings.ToLower(v.GetTLSModeInUse(HTTPSNMATLSConfigName))
}

func (v *VerticaDB) GetClientServerTLSModeInUse() string {
	return strings.ToLower(v.GetTLSModeInUse(ClientServerTLSConfigName))
}

// SetTLSConfigs updates the slice with a new TLSConfig by Name, and returns true if any changes occurred.
func SetTLSConfigs(refs *[]TLSConfigStatus, newRef *TLSConfigStatus) (changed bool) {
	existing := FindTLSConfig(*refs, "Name", newRef.Name)
	if existing == nil {
		*refs = append(*refs, *newRef)
		return true
	}

	if existing.Secret != newRef.Secret {
		existing.Secret = newRef.Secret
		changed = true
	}
	if existing.Mode != newRef.Mode {
		existing.Mode = newRef.Mode
		changed = true
	}
	if !newRef.LastUpdate.IsZero() && existing.LastUpdate != newRef.LastUpdate {
		existing.LastUpdate = newRef.LastUpdate
		changed = true
	}
	if newRef.AutoRotateSecrets != nil {
		existing.AutoRotateSecrets = newRef.AutoRotateSecrets
		changed = true
	}
	if existing.AutoRotateFailedSecret != newRef.AutoRotateFailedSecret {
		existing.AutoRotateFailedSecret = newRef.AutoRotateFailedSecret
		changed = true
	}

	return changed
}

func convertToBool(src string) bool {
	converted := false
	_, err := strconv.ParseBool(src)
	if err == nil {
		converted = true
	}
	return converted
}

func convertToInt(src string) (int, bool) {
	converted := false
	varAsInt, err := strconv.ParseInt(src, 10, 0)
	if err == nil {
		converted = true
	}
	return int(varAsInt), converted
}

func hasValidIntAnnotation(allErrs field.ErrorList, annotationName string, val int) field.ErrorList {
	if val < 0 {
		err := field.Invalid(field.NewPath("metadata").Child("annotations").Child(annotationName),
			val, fmt.Sprintf("%s must be non-negative", annotationName))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// Check for invalid characters in an object name (such as DB or archive name)
func findInvalidChars(objName string, allowDash bool) string {
	invalidChars := invalidNameChars

	// Dash is supported in some object names (eg archive name) but not others (eg db name)
	if !allowDash {
		invalidChars += "-"
	}

	foundChars := ""
	for _, c := range invalidChars {
		if strings.Contains(objName, string(c)) {
			foundChars += string(c)
		}
	}
	return foundChars
}

func (v *VerticaDB) GetSpecHTTPSNMATLSMode() string {
	if v.Spec.HTTPSNMATLS == nil {
		return ""
	}
	return v.Spec.HTTPSNMATLS.Mode
}

func (v *VerticaDB) GetHTTPSNMATLSMode() string {
	return strings.ToLower(v.GetSpecHTTPSNMATLSMode())
}

// Get HTTPSNMATLS secret from spec or return "" if not found
func (v *VerticaDB) GetHTTPSNMATLSSecret() string {
	if v.Spec.HTTPSNMATLS == nil {
		return ""
	}
	return v.Spec.HTTPSNMATLS.Secret
}

// GetNMATLSSecret returns the NMATLS secret based on enable-tls annotation
func (v *VerticaDB) GetNMATLSSecret() string {
	if !vmeta.UseTLSAuth(v.Annotations) {
		return v.Spec.NMATLSSecret
	}
	return v.GetHTTPSNMATLSSecret()
}

func (v *VerticaDB) GetSpecClientServerTLSMode() string {
	if v.Spec.ClientServerTLS == nil {
		return ""
	}
	return v.Spec.ClientServerTLS.Mode
}

func (v *VerticaDB) GetClientServerTLSMode() string {
	return strings.ToLower(v.GetSpecClientServerTLSMode())
}

// Get ClientServerTLS secret from spec or return "" if not found
func (v *VerticaDB) GetClientServerTLSSecret() string {
	if v.Spec.ClientServerTLS == nil {
		return ""
	}
	return v.Spec.ClientServerTLS.Secret
}

// Check if TLS not enabled, DB not initialized, or rotate has failed (and rollback is not in progress).
// In these cases, we skip TLS Update
func (v *VerticaDB) ShouldSkipTLSUpdateReconcile() bool {
	return !v.IsSetForTLS() ||
		!v.IsDBInitialized() ||
		v.IsTLSCertRollbackNeeded()
}

// HasNoExtraEnv returns true if there are no extra environment variables
// or envFrom specified in the VerticaDB spec.
func (v *VerticaDB) HasNoExtraEnv() bool {
	return len(v.Spec.ExtraEnv) == 0 && len(v.Spec.EnvFrom) == 0
}

// MakeSourceVDBName is a helper that creates a sample name for the source VerticaDB for test purposes
func MakeSourceVDBName() types.NamespacedName {
	return types.NamespacedName{Name: "vertica-source-sample", Namespace: "default"}
}

// MakeTargetVDBName is a helper that creates a sample name for the target VerticaDB for test purposes
func MakeTargetVDBName() types.NamespacedName {
	return types.NamespacedName{Name: "vertica-target-sample", Namespace: "default"}
}

// IsOtherSubclusterDraining returns true if any subcluster drain annotation
// exists that has a suffix different from the given scName.
func (v *VerticaDB) IsOtherSubclusterDraining(scName string) bool {
	drainAnnotations, found := vmeta.FindDrainTimeoutSubclusterAnnotations(v.Annotations)
	if !found {
		return false
	}
	for _, annotation := range drainAnnotations {
		// If we have an annotation that is NOT for this scName,
		// it means another subcluster is draining.
		if annotation != vmeta.GenSubclusterDrainStartAnnotationName(scName) {
			return true
		}
	}
	return false
}

// GetPrometheusScrapeDuration returns the Prometheus scrape duration as a string
func (v *VerticaDB) GetPrometheusScrapeDuration() string {
	if vmeta.GetPrometheusScrapeInterval(v.Annotations) == 0 {
		return ""
	}

	return fmt.Sprintf("%ds", vmeta.GetPrometheusScrapeInterval(v.Annotations))
}

// MakeVersionStrForOpVersion can convert operator version to vertica version format
func MakeVersionStrForOpVersion(v string) string {
	if v == "" {
		return ""
	}
	return "v" + v
}

// equalStringSlices compares two string arrays, slice by slice
func (v *VerticaDB) EqualStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// IsHTTPPollingError checks if the error is related to HTTPS polling
func (v *VerticaDB) IsHTTPPollingError(err error) bool {
	errMsg := err.Error()

	if strings.Contains(errMsg, "HTTPSPollCertificateHealthOp") {
		return true
	}

	if strings.Contains(errMsg, "failed to poll https") {
		return true
	}

	return false
}
