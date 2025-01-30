/*
 (c) Copyright [2023-2024] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package util

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
	"golang.org/x/sys/unix"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type FetchAllEnvVars interface {
	SetK8Secrets(port, secretNameSpace, secretName string)
	SetK8Certs(rootCAPath, certPath, keyPath string)
	TypeName() string
}

const (
	RootDir               = "/"
	NodeInfoCountMismatch = "[%s] expect one node's information, but got %d nodes' information from HTTPS /v1/nodes/<host> endpoint on host %s"
	DepotSizeHint         = "integer%, which expresses the depot size as a percentage of the total disk size."
	DepotSizeKMGTMsg      = "integer{K|M|G|T}, where K is kilobytes, M is megabytes, G is gigabytes, and T is terabytes.\n"
	DepotFmtMsg           = "Size of depot in one of the following formats:\n"
	TimeToWaitToClose     = "The time to wait, in seconds, for user connections to close on their own.\n"
	TimeExpire            = "When the time expires, user connections are automatically closed and the database is hut down.\n"
	InfiniteWaitTime      = "If the value is negative, VCluster waits indefinitely until all user connections close."
	CloseAllConns         = "If set to 0, VCluster closes all user connections immediately.\n"
	Default               = "Default: "
	FailToWriteToConfig   = "Failed to write the configuration file: "
	CallCommand           = "Calling method Run() for command "
	DBInfo                = "because we cannot retrieve the correct database information"
	CommStorageLoc        = "communal storage location is not specified"
	CommStorageFail       = "failed to retrieve the communal storage location"
	SubclustersEndpoint   = "subclusters/"
	ShutDownEndpoint      = "/shutdown"
	NodesEndpoint         = "nodes/"
	DropEndpoint          = "/drop"
	ArchiveEndpoint       = "archives"
	LicenseEndpoint       = "license"
	TLSAuthEndpoint       = "authentication/tls/"
)

const (
	keyValueArrayLen = 2
	ipv4Str          = "IPv4"
	ipv6Str          = "IPv6"
	AWSAuthKey       = "awsauth"
	kubernetesPort   = "KUBERNETES_PORT"

	// Environment variable names storing name of k8s secret that has NMA cert
	secretNameSpaceEnvVar = "NMA_SECRET_NAMESPACE"
	secretNameEnvVar      = "NMA_SECRET_NAME"

	// Environment variable names for locating the NMA certs located in the file system
	nmaRootCAPathEnvVar = "NMA_ROOTCA_PATH"
	nmaCertPathEnvVar   = "NMA_CERT_PATH"
	nmaKeyPathEnvVar    = "NMA_KEY_PATH"

	objectNameUnsupportedCharacters = `=<>'^\".@?#&/:;{}()[] \~!%+|,` + "`$"
)

const (
	// Unbound nodes are the nodes in catalog but without IP assigned.
	// These nodes can come from the following scenario:
	// - a database has primary and secondary nodes
	// - users run revive_db to the primary nodes only
	// - the secondary nodes become "unbound nodes" after this revive
	// - users need to run start_node with re-ip to bring the unbound nodes up
	UnboundedIPv4 = "0.0.0.0"
	UnboundedIPv6 = "0:0:0:0:0:0:0:0"
)

// NmaSecretLookup retrieves kubernetes secrets.
func NmaSecretLookup(f FetchAllEnvVars) {
	k8port, _ := os.LookupEnv(kubernetesPort)
	secretNameSpace, _ := os.LookupEnv(secretNameSpaceEnvVar)
	secretName, _ := os.LookupEnv(secretNameEnvVar)
	f.SetK8Secrets(k8port, secretNameSpace, secretName)
}

// NmaCertsLookup retrieves kubernetes certs.
func NmaCertsLookup(f FetchAllEnvVars) {
	rootCAPath, _ := os.LookupEnv(nmaRootCAPathEnvVar)
	certPath, _ := os.LookupEnv(nmaCertPathEnvVar)
	keyPath, _ := os.LookupEnv(nmaKeyPathEnvVar)
	f.SetK8Certs(rootCAPath, certPath, keyPath)
}

func GetJSONLogErrors(responseContent string, responseObj any, opName string, logger vlog.Printer) error {
	err := json.Unmarshal([]byte(responseContent), responseObj)
	if err != nil {
		opTag := ""
		if opName != "" {
			opTag = fmt.Sprintf("[%s] ", opName)
		}

		logger.Error(err, "op name", opTag, "fail to unmarshal the response content")
		return err
	}

	return nil
}

func CheckNotEmpty(a string) bool {
	return a != ""
}

func BoolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func CheckAllEmptyOrNonEmpty(vars ...string) bool {
	// Initialize flags for empty and non-empty conditions
	allEmpty := true
	allNonEmpty := true

	// Check each string variable
	for _, v := range vars {
		if v != "" {
			allEmpty = false
		} else {
			allNonEmpty = false
		}
	}

	// Return true if either all are empty or all are non-empty
	return allEmpty || allNonEmpty
}

// delete keys in the given iterable from the given map
func DeleteKeysFromMap[K comparable, V any](m map[K]V, keys []K) map[K]V {
	for _, key := range keys {
		delete(m, key)
	}
	return m
}

// Creates and returns a deep copy of the given map
func CloneMap[K comparable, V any](original map[K]V, cloneValue func(V) V) map[K]V {
	clone := make(map[K]V, len(original))
	for key, value := range original {
		clone[key] = cloneValue(value)
	}
	return clone
}

// calculate array diff: m-n
func SliceDiff[K comparable](m, n []K) []K {
	nSet := make(map[K]struct{}, len(n))
	for _, x := range n {
		nSet[x] = struct{}{}
	}

	var diff []K
	for _, x := range m {
		if _, found := nSet[x]; !found {
			diff = append(diff, x)
		}
	}
	return diff
}

// calculate and sort array commonalities: m ∩ n
func SliceCommon[K constraints.Ordered](m, n []K) []K {
	mSet := mapset.NewSet[K](m...)
	nSet := mapset.NewSet[K](n...)
	common := mSet.Intersect(nSet).ToSlice()
	slices.Sort(common)

	return common
}

// calculate diff of map keys: m-n
func MapKeyDiff[M ~map[K]V, K comparable, V any](m, n M) []K {
	var diff []K

	for k := range m {
		if _, found := n[k]; !found {
			diff = append(diff, k)
		}
	}

	return diff
}

// FilterMapByKey, given a map and a slice of keys, returns a map,
// which is a subset of the original, that contains only keys in
// from the given slice.
func FilterMapByKey[M ~map[K]V, K comparable, V any](m M, n []K) M {
	result := make(M)
	for _, k := range n {
		if v, found := m[k]; found {
			result[k] = v
		}
	}
	return result
}

func CheckPathExist(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

func StringInArray(str string, list []string) bool {
	return slices.Contains(list, str)
}

// convert an array to a string by joining the elements in the array
// using the given delimiter
func ArrayToString(arr []string, delimiter string) string {
	return strings.Join(arr, delimiter)
}

func GetCurrentUsername() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", err
	}
	return currentUser.Username, nil
}

// avoid import strings in every operation
func TrimSpace(str string) string {
	return strings.TrimSpace(str)
}

// have this util function so no need to import file/path
// for every command that needs check file path
func IsAbsPath(path string) bool {
	return filepath.IsAbs(path)
}

func ResolveToAbsPath(path string) (string, error) {
	if !strings.Contains(path, "~") {
		return filepath.Abs(path)
	}
	// needed for resolving '~' in relative paths
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	homeDir := usr.HomeDir

	if path == "~" {
		return homeDir, nil
	} else if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:]), nil
	}
	return "", fmt.Errorf("invalid path")
}

// IP util functions
func IsIPv4(ip string) bool {
	// To4() may not return nil even if the given address is ipv6
	// we need to double check whether the ip string contains `:`
	return !strings.Contains(ip, ":") && net.ParseIP(ip).To4() != nil
}

func IsIPv6(ip string) bool {
	// To16() may not return nil even if the given address is ipv4
	// we need to double check whether the ip string contains `:`
	return strings.Contains(ip, ":") && net.ParseIP(ip).To16() != nil
}

func AddressCheck(address string, ipv6 bool) error {
	checkPassed := false
	if ipv6 {
		checkPassed = IsIPv6(address)
	} else {
		checkPassed = IsIPv4(address)
	}

	if !checkPassed {
		ipVersion := ipv4Str
		if ipv6 {
			ipVersion = ipv6Str
		}
		return fmt.Errorf("%s in the re-ip file is not a valid %s address", address, ipVersion)
	}

	if address == UnboundedIPv4 || address == UnboundedIPv6 {
		return errors.New("the re-ip list should not contain unbound addresses")
	}

	return nil
}

func ResolveToIPAddrs(hostname string, ipv6 bool) ([]string, error) {
	// resolve hostname using local resolver
	hostIPs, err := net.LookupHost(hostname)
	if err != nil {
		return nil, err
	}
	if len(hostIPs) < 1 {
		return nil, fmt.Errorf("cannot resolve %s to a valid IP address", hostname)
	}
	var v4Addrs []string
	var v6Addrs []string
	for _, addr := range hostIPs {
		if IsIPv4(addr) {
			v4Addrs = append(v4Addrs, addr)
		} else if IsIPv6(addr) {
			v6Addrs = append(v6Addrs, addr)
		} else {
			return nil, fmt.Errorf("%s is resolved to invalid address %s", hostname, addr)
		}
	}
	if ipv6 {
		return v6Addrs, nil
	}
	return v4Addrs, nil
}

func ResolveToOneIP(hostname string, ipv6 bool) (string, error) {
	// already an IPv4 or IPv6 address
	if !ipv6 && IsIPv4(hostname) {
		return hostname, nil
	}
	// IPv6
	if ipv6 && IsIPv6(hostname) {
		return hostname, nil
	}

	// resolve host name to address
	addrs, err := ResolveToIPAddrs(hostname, ipv6)
	// contains the case where the hostname cannot be resolved to be IP
	if err != nil {
		return "", err
	}

	ipVersion := ipv4Str
	if ipv6 {
		ipVersion = ipv6Str
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("cannot resolve %s as %s address", hostname, ipVersion)
	}

	if len(addrs) > 1 {
		return "", fmt.Errorf("%s is resolved to more than one IP addresss: %v", hostname, addrs)
	}

	return addrs[0], nil
}

// resolve RawHosts to be IP addresses
func ResolveRawHostsToAddresses(rawHosts []string, ipv6 bool) ([]string, error) {
	var hostAddresses []string

	for _, host := range rawHosts {
		if host == "" {
			return hostAddresses, fmt.Errorf("invalid empty host found in the provided host list")
		}
		if host == UnboundedIPv4 || host == UnboundedIPv6 {
			return hostAddresses, fmt.Errorf("ambiguous host address (%s) is used", host)
		}
		addr, err := ResolveToOneIP(host, ipv6)
		if err != nil {
			return hostAddresses, err
		}
		// use a list to respect user input order
		hostAddresses = append(hostAddresses, addr)
	}

	return hostAddresses, nil
}

// replace all '//' to be '/', trim the path string
func GetCleanPath(path string) string {
	if path == "" {
		return path
	}
	cleanPath := strings.TrimSpace(path)
	// clean and normalize the path
	cleanPath = filepath.Clean(cleanPath)
	return cleanPath
}

func AbsPathCheck(dirPath string) error {
	if !filepath.IsAbs(dirPath) {
		return fmt.Errorf("'%s' is not an absolute path", dirPath)
	}
	return nil
}

// ParseHostList will trim spaces and convert all chars to lowercase in the hosts
func ParseHostList(hosts *[]string) error {
	var parsedHosts []string
	for _, host := range *hosts {
		parsedHost := strings.TrimSpace(strings.ToLower(host))
		if parsedHost != "" {
			parsedHosts = append(parsedHosts, parsedHost)
		}
	}
	if len(parsedHosts) == 0 {
		return fmt.Errorf("must specify a host or host list")
	}

	*hosts = parsedHosts
	return nil
}

// get env var with a fallback value
func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// get int value of env var with a fallback value
func GetEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
		// failed to retrieve env value, should use fallback value
	}
	return fallback
}

func CheckMissingFields(object any) error {
	var missingFields []string
	v := reflect.ValueOf(object)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			missingFields = append(missingFields, v.Type().Field(i).Name)
		}
	}
	if len(missingFields) > 0 {
		return fmt.Errorf("unexpected or missing fields in response object: %v", missingFields)
	}
	return nil
}

// when password is given, the user name cannot be empty
func ValidateUsernameAndPassword(opName string, useHTTPPassword bool, userName string) error {
	if useHTTPPassword && userName == "" {
		return fmt.Errorf("[%s] should provide a username for using basic authentication for HTTPS requests", opName)
	}
	return nil
}

const (
	FileExist    = 0
	FileNotExist = 1
	NoWritePerm  = 2
	// this can be extended
	// if we want to check other permissions
)

// Check whether the directory is read accessible
// by trying to open the file
func CanReadAccessDir(dirPath string) error {
	if _, err := os.Open(dirPath); err != nil {
		return fmt.Errorf("read access denied to path [%s]", dirPath)
	}
	return nil
}

// Check whether the directory is read accessible
func CanWriteAccessPath(path string) int {
	// check whether the path exists
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileNotExist
		}
	}

	// check whether the path has write access
	if err := unix.Access(path, unix.W_OK); err != nil {
		log.Printf("Path '%s' is not writable.\n", path)
		return NoWritePerm
	}

	return FileExist
}

// copy file from a source path to a destination path
func CopyFile(sourcePath, destinationPath string, perm fs.FileMode) error {
	fileBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}

	err = os.WriteFile(destinationPath, fileBytes, perm)
	if err != nil {
		return fmt.Errorf("fail to create file at %s", destinationPath)
	}

	return nil
}

// check if an option is passed in
func IsOptionSet(f *flag.FlagSet, optionName string) bool {
	found := false
	f.Visit(func(f *flag.Flag) {
		if f.Name == optionName {
			found = true
		}
	})
	return found
}

// ValidateName will validate the name of an obj, the obj can be database, subcluster, etc.
// when a name is provided, make sure no special chars are in it
func ValidateName(name, obj string, allowDash bool) error {
	escapeChars := objectNameUnsupportedCharacters + "*"
	if !allowDash {
		escapeChars += "-"
	}
	for _, c := range name {
		if strings.Contains(escapeChars, string(c)) {
			return fmt.Errorf("invalid character in %s name: %c", obj, c)
		}
	}
	return nil
}

// ValidateQualifiedObjectNamePattern will validate the pattern of [.namespace].schema.table, separated by ","
// Return nil when its valid, else will panic
func ValidateQualifiedObjectNamePattern(pattern string, allowAsterisk bool) error {
	const maxPatternLen = 128

	// Build a regex that matches any unsupported characters
	disallowedChars := objectNameUnsupportedCharacters
	if !allowAsterisk {
		disallowedChars += "*"
	}
	disallowedCharsRegex := regexp.QuoteMeta(disallowedChars)

	// Validates [.namespace].schema.table format and disallows any special characters
	// Ref: https://docs.vertica.com/24.1.x/en/sql-reference/language-elements/identifiers/
	qualifiedObjectNameRegex := fmt.Sprintf(`^(\.[^%s]+\.)?([^%s]+\.)?[^%s]+$`,
		disallowedCharsRegex, disallowedCharsRegex, disallowedCharsRegex)
	r := regexp.MustCompile(qualifiedObjectNameRegex)

	objects := strings.Split(pattern, ",")
	for _, obj := range objects {
		// start with v_ is invalid
		if strings.HasPrefix(obj, "v_") {
			return fmt.Errorf("invalid character in pattern %s: %s", pattern, obj)
		}
		// len > 128 is invalid
		if len([]rune(obj)) > maxPatternLen {
			return fmt.Errorf("pattern is too long %s: %s", pattern, obj)
		}
		match := r.MatchString(obj)
		if !match {
			return fmt.Errorf("invalid pattern %s: %s", pattern, obj)
		}
	}
	return nil
}

func ValidateDBName(dbName string) error {
	return ValidateName(dbName, "database", false)
}

func ValidateScName(dbName string) error {
	return ValidateName(dbName, "subcluster", true)
}

func ValidateSandboxName(dbName string) error {
	return ValidateName(dbName, "sandbox", true)
}

func ValidateArchiveName(archive string) error {
	return ValidateName(archive, "archive", true)
}

// suppress help message for hidden options
func SetParserUsage(parser *flag.FlagSet, op string) {
	fmt.Printf("Usage of %s:\n", op)
	fmt.Println("Options:")
	parser.VisitAll(func(f *flag.Flag) {
		if f.Usage != SuppressHelp {
			fmt.Printf("  -%s\n\t%s\n", f.Name, f.Usage)
		}
	})
}

func GetEonFlagMsg(message string) string {
	return "[Eon only] " + message
}

func ValidateAbsPath(path, pathName string) error {
	err := AbsPathCheck(path)
	if err != nil {
		return fmt.Errorf("must specify an absolute %s", pathName)
	}

	return nil
}

// ValidateRequiredAbsPath check whether a required path is set
// then validate it
func ValidateRequiredAbsPath(path, pathName string) error {
	if path == "" {
		return fmt.Errorf("must specify an absolute %s", pathName)
	}

	return ValidateAbsPath(path, pathName)
}

func ParamNotSetErrorMsg(param string) error {
	return fmt.Errorf("%s is pointed to nil", param)
}

// GenVNodeName generates a vnode and returns it after checking it is not already
// taken by an existing node.
func GenVNodeName(vnodes map[string]string, dbName string, hostCount int) (string, bool) {
	dbNameInNode := strings.ToLower(dbName)
	for i := 0; i < hostCount; i++ {
		nodeNameSuffix := i + 1
		vname := fmt.Sprintf("v_%s_node%04d", dbNameInNode, nodeNameSuffix)
		if _, ok := vnodes[vname]; !ok {
			// we have found an available vnode name
			return vname, true
		}
	}
	return "", false
}

// CopySlice returns a copy of a slice.
func CopySlice[T any](original []T) []T {
	if original == nil {
		return nil
	}

	var copyOfList = make([]T, len(original))
	copy(copyOfList, original)

	return copyOfList
}

// CopyMap returns a copy of a map.
func CopyMap[K comparable, V any](original map[K]V) map[K]V {
	if original == nil {
		return nil
	}

	copyOfMap := make(map[K]V)

	for key, value := range original {
		copyOfMap[key] = value
	}

	return copyOfMap
}

// ValidateCommunalStorageLocation can identify some invalid communal storage locations
func ValidateCommunalStorageLocation(location string) error {
	// reject empty communal storage location
	if location == "" {
		return fmt.Errorf("must specify a communal storage location")
	}

	// create a regex to accept valid urls like "s3://vertica-fleeting/k8s/revive_eon_5"
	re := regexp.MustCompile("^[0-9a-zA-Z]+://[^/]+(/[^/]+)*/?$")
	// check if communal location is a valid local path or a valid remote url path
	if !IsAbsPath(location) && !re.MatchString(location) {
		return fmt.Errorf("communal storage path is invalid: use an absolute local path or a correct remote url path")
	}

	return nil
}

// GetPathPrefix returns a path prefix for a (catalog/data/depot) path of a node
func GetPathPrefix(path string) string {
	if path == "" {
		return path
	}
	return filepath.Dir(filepath.Dir(path))
}

// default date time format: this omits nanoseconds but is still able to parse those out
const DefaultDateTimeFormat = time.DateTime

// default date time format: this includes nanoseconds
const DefaultDateTimeNanoSecFormat = time.DateTime + ".000000000"

// default date only format: this omits time within a date
const DefaultDateOnlyFormat = time.DateOnly

// import time package in this util file so other files don't need to import time
// wrapper function to handle empty input string, returns an error if the time is invalid
// caller responsible for passing in correct layout
func IsEmptyOrValidTimeStr(layout, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsedTime, err := time.Parse(layout, value)
	if err != nil {
		return nil, err
	}
	return &parsedTime, nil
}

func fillInDefaultTimeForTimestampHelper(parsedDate time.Time, hour, minute, second,
	nanosecond int) (string, time.Time) {
	year, month, day := parsedDate.Year(), parsedDate.Month(), parsedDate.Day()
	location := parsedDate.Location() // Extracting the timezone
	datetime := time.Date(year, month, day, hour, minute, second, nanosecond, location)
	formatedDatetime := datetime.Format(DefaultDateTimeNanoSecFormat)
	return formatedDatetime, datetime
}

// Read date only string from argument, fill in time, overwrite argument by date time string, and return parsed time,
// the filled in time will indicate the beginning of a day
func FillInDefaultTimeForStartTimestamp(dateonly *string) *time.Time {
	parsedDate, _ := time.Parse(DefaultDateOnlyFormat, *dateonly)
	formatedDatetime, datetime := fillInDefaultTimeForTimestampHelper(parsedDate, 0, 0, 0, 0)
	*dateonly = formatedDatetime
	return &datetime
}

// Read date only string from argument, fill in time, overwrite argument by date time string, and return parsed time,
// the filled in time will indicate the end of a day (right before the beginning of the following day)
func FillInDefaultTimeForEndTimestamp(dateonly *string) *time.Time {
	parsedDate, _ := time.Parse(DefaultDateOnlyFormat, *dateonly)
	const lastHour = 23
	const lastMin = 59
	const lastSec = 59
	const lastNanoSec = 999999999
	formatedDatetime, datetime := fillInDefaultTimeForTimestampHelper(parsedDate, lastHour, lastMin, lastSec, lastNanoSec)
	*dateonly = formatedDatetime
	return &datetime
}

func IsTimeEqualOrAfter(start, end time.Time) bool {
	return end.Equal(start) || end.After(start)
}

const EmptyConfigParamErrMsg = "configuration parameter must not be empty"

func IsK8sEnvironment() bool {
	port, portSet := os.LookupEnv(kubernetesPort)
	return portSet && port != ""
}

// GetClusterName can return the correct cluster name based on the sandbox name.
// It can help people to log the cluster name.
func GetClusterName(sandbox string) string {
	if sandbox == MainClusterSandbox {
		return "main cluster"
	}
	return "sandbox " + sandbox
}
