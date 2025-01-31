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
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tonglil/buflogr"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type NMAHealthOpResponse map[string]string

const InvalChar = "invalid character in "

func redirectLog() (*bytes.Buffer, vlog.Printer) {
	// redirect log to a local bytes.Buffer
	var logBuffer bytes.Buffer
	log := buflogr.NewWithBuffer(&logBuffer)
	vlogger := vlog.Printer{}
	vlogger.Log = log

	return &logBuffer, vlogger
}

func TestGetJSONLogErrors(t *testing.T) {
	/* positive case
	 */
	resultContent := `{"healthy": "true"}`
	var responseObj NMAHealthOpResponse
	expectedResponseObj := NMAHealthOpResponse{"healthy": "true"}

	err := GetJSONLogErrors(resultContent, &responseObj, "", vlog.Printer{})

	assert.Nil(t, err)
	assert.Equal(t, responseObj, expectedResponseObj)

	/* netative case
	 */
	logBuffer, log := redirectLog()

	resultContent = `{"healthy": 123}`
	err = GetJSONLogErrors(resultContent, &responseObj, "", log)

	assert.NotNil(t, err)
	assert.Contains(t, logBuffer.String(),
		"ERROR json: cannot unmarshal number into Go value of type string op name  fail to unmarshal the response content")

	err = GetJSONLogErrors(resultContent, &responseObj, "NMAHealthOp", log)
	assert.NotNil(t, err)
	assert.Contains(t, logBuffer.String(),
		"ERROR json: cannot unmarshal number into Go value of type string op name [NMAHealthOp] fail to unmarshal the response content")
}

func TestStringInArray(t *testing.T) {
	list := []string{"str1", "str2", "str3"}

	// positive case
	str := "str1"
	found := StringInArray(str, list)
	assert.Equal(t, found, true)

	// negative case
	strNeg := "randomStr"
	found = StringInArray(strNeg, list)
	assert.Equal(t, found, false)
}

func TestResolveToAbsPath(t *testing.T) {
	// positive case
	// not testing ~ because the output depends on devjail users
	path := "/data"
	res, err := ResolveToAbsPath(path)
	assert.Nil(t, err)
	assert.Equal(t, path, res)

	// negative case
	path = "/data/~/test"
	res, err = ResolveToAbsPath(path)
	assert.NotNil(t, err)
	assert.Equal(t, "", res)
}

func TestResolveToOneIP(t *testing.T) {
	// positive case
	hostname := "192.168.1.1"
	res, err := ResolveToOneIP(hostname, false)
	assert.Nil(t, err)
	assert.Equal(t, res, hostname)

	// negative case
	hostname = "randomIP"
	res, err = ResolveToOneIP(hostname, false)
	assert.NotNil(t, err)
	assert.Equal(t, res, "")

	// given IP does not match its IP version
	_, err = ResolveToOneIP("192.168.1.101", true)
	assert.ErrorContains(t, err, "cannot resolve 192.168.1.101 as IPv6 address")

	_, err = ResolveToOneIP("2001:db8::8:800:200c:417a", false)
	assert.ErrorContains(t, err, "cannot resolve 2001:db8::8:800:200c:417a as IPv4 address")
}

func TestGetCleanPath(t *testing.T) {
	// positive cases
	path := ""
	res := GetCleanPath(path)
	assert.Equal(t, res, "")

	path = "//data"
	res = GetCleanPath(path)
	assert.Equal(t, res, "/data")

	path = "///data"
	res = GetCleanPath(path)
	assert.Equal(t, res, "/data")

	path = "////data"
	res = GetCleanPath(path)
	assert.Equal(t, res, "/data")

	path = "///data/"
	res = GetCleanPath(path)
	assert.Equal(t, res, "/data")

	path = "//scratch_b/qa/data/"
	res = GetCleanPath(path)
	assert.Equal(t, res, "/scratch_b/qa/data")

	path = "//data "
	res = GetCleanPath(path)
	assert.Equal(t, res, "/data")
}

func TestParseHostList(t *testing.T) {
	// positive case
	hosts := []string{" vnode1 ", " vnode2", "vnode3 ", "  "}
	err := ParseHostList(&hosts)
	expected := []string{"vnode1", "vnode2", "vnode3"}
	assert.Nil(t, err)
	assert.Equal(t, hosts, expected)

	// negative case
	hosts = []string{"  "}
	err = ParseHostList(&hosts)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "must specify a host or host list")
}

type testStruct struct {
	Field1 string
	Field2 int
	Field3 []int
}

func TestCheckMissingFields(t *testing.T) {
	/* negative cases
	 */
	testObj := testStruct{}
	err := CheckMissingFields(testObj)
	assert.ErrorContains(t, err, "unexpected or missing fields in response object: [Field1 Field2 Field3]")

	testObj.Field1 = "Value 1"
	err = CheckMissingFields(testObj)
	assert.ErrorContains(t, err, "unexpected or missing fields in response object: [Field2 Field3]")

	/* positive case
	 */
	testObj.Field2 = 2
	testObj.Field3 = []int{3, 4, 5}
	err = CheckMissingFields(testObj)
	assert.Nil(t, err)
}

func TestSliceDiff(t *testing.T) {
	a := []string{"1", "2"}
	b := []string{"1", "3", "4"}
	expected := []string{"2"}
	actual := SliceDiff(a, b)
	assert.Equal(t, expected, actual)
}

func TestSliceCommon(t *testing.T) {
	a := []string{"3", "5", "4", "1", "2"}
	b := []string{"5", "6", "7", "4", "3"}
	expected := []string{"3", "4", "5"}
	actual := SliceCommon(a, b)
	assert.Equal(t, expected, actual)
}

func TestMapKeyDiff(t *testing.T) {
	a := map[string]bool{"1": true, "2": true}
	b := map[string]bool{"1": true, "3": true, "4": false}

	expected := []string{"2"}
	actual := MapKeyDiff(a, b)
	assert.Equal(t, expected, actual)
}

func TestFilterMapByKey(t *testing.T) {
	a := map[string]int{"1": 1, "2": 2}
	b := map[string]int{"1": 1, "3": 3, "4": 4, "2": 2}
	keys := []string{"1", "2"}
	c := FilterMapByKey(b, keys)
	assert.EqualValues(t, a, c)
}

func TestGetEnv(t *testing.T) {
	key := "NO_SUCH_ENV"
	fallback := "test"
	actual := GetEnv(key, fallback)
	assert.Equal(t, fallback, actual)
}

func TestValidateUsernamePassword(t *testing.T) {
	// when user name is "" but use password, the check should fail
	err := ValidateUsernameAndPassword("mock_op", true, "")
	assert.Error(t, err)

	// when user name is not empty and use password, the check should succeed
	err = ValidateUsernameAndPassword("mock_op", true, "dkr_dbadmin")
	assert.NoError(t, err)
}

func TestNewErrorFormatVerb(t *testing.T) {
	err := errors.New("test error")
	// replace %s with %w case 1
	oldErr1 := fmt.Errorf("fail to read config file, details: %s", err)
	newErr1 := fmt.Errorf("fail to read config file, details: %w", err)
	assert.EqualError(t, oldErr1, newErr1.Error())

	// replace %s with %w case 2
	oldErr2 := fmt.Errorf("fail to marshal config data, details: %s", err.Error())
	newErr2 := fmt.Errorf("fail to marshal config data, details: %w", err)
	assert.EqualError(t, oldErr2, newErr2.Error())

	// replace %v with %w
	oldErr3 := fmt.Errorf("fail to marshal start command, %v", err)
	newErr3 := fmt.Errorf("fail to marshal start command, %w", err)
	assert.EqualError(t, oldErr3, newErr3.Error())
}

func TestValidateName(t *testing.T) {
	// positive cases
	obj := "database"
	err := ValidateName("test_db", obj, false)
	assert.Nil(t, err)

	err = ValidateName("db1", obj, false)
	assert.Nil(t, err)

	// negative cases
	err = ValidateName("test$db", obj, false)
	assert.ErrorContains(t, err, InvalChar+obj+" name: $")

	err = ValidateName("[db1]", obj, false)
	assert.ErrorContains(t, err, InvalChar+obj+" name: [")

	err = ValidateName("!!??!!db1", obj, false)
	assert.ErrorContains(t, err, InvalChar+obj+" name: !")

	err = ValidateName("test-db", obj, false)
	assert.ErrorContains(t, err, InvalChar+obj+" name: -")

	err = ValidateName("test-db", obj, true)
	assert.Nil(t, err)

	err = ValidateName("0test-db", obj, true)
	assert.Nil(t, err)
}

func TestValidateQualifiedObjectNamePattern(t *testing.T) {
	// positive cases
	obj := "schema.database"
	err := ValidateQualifiedObjectNamePattern(obj, true)
	assert.Nil(t, err)

	obj = "schema.*"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.Nil(t, err)

	obj = "*.database"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.Nil(t, err)

	obj = "valid.valid,valid.valid"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.Nil(t, err)

	const matchAnySchemaTable = "*.*"
	err = ValidateQualifiedObjectNamePattern(matchAnySchemaTable, true)
	assert.Nil(t, err)

	const matchAnyTable = "*"
	err = ValidateQualifiedObjectNamePattern(matchAnyTable, true)
	assert.Nil(t, err)

	obj = ".namespace.*.*"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.Nil(t, err)

	obj = ".namespace.schema.table"
	err = ValidateQualifiedObjectNamePattern(obj, false)
	assert.Nil(t, err)

	// negative cases

	const (
		invalidCharacter = "invalid character in pattern "
		invalidPattern   = "invalid pattern "
	)

	obj = "v_invalid.valid"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.ErrorContains(t, err, invalidCharacter+obj+": v_invalid.valid")

	obj = "valid.valid,v_invalid_name.valid"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.ErrorContains(t, err, invalidCharacter+obj+": v_invalid_name.valid")

	obj = `valid.v_invalid_TO_loooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooo
	oooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooo
	oooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong`
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.ErrorContains(t, err, "pattern is too long "+obj+
		`: valid.v_invalid_TO_loooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooo
	oooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooo
	oooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong`)

	obj = "no_leading_dot.*.*"
	err = ValidateQualifiedObjectNamePattern(obj, true)
	assert.ErrorContains(t, err, invalidPattern+obj+": no_leading_dot.*.*")

	obj = ".wildcards.*.*"
	err = ValidateQualifiedObjectNamePattern(obj, false)
	assert.ErrorContains(t, err, invalidPattern+obj+": .wildcards.*.*")

	err = ValidateQualifiedObjectNamePattern(matchAnySchemaTable, false)
	assert.ErrorContains(t, err, invalidPattern+matchAnySchemaTable+": *.*")

	err = ValidateQualifiedObjectNamePattern(matchAnyTable, false)
	assert.ErrorContains(t, err, invalidPattern+matchAnyTable+": *")
}

func TestSetEonFlagHelpMsg(t *testing.T) {
	msg := "Path to depot directory"
	finalMsg := "[Eon only] Path to depot directory"
	assert.Equal(t, GetEonFlagMsg(msg), finalMsg)
}

func TestGenVNodeName(t *testing.T) {
	dbName := "test_db"
	// returns vnode
	vnodes := make(map[string]string)
	vnodes["v_test_db_node0002"] = "vnode1"
	totalCount := 2
	vnode, ok := GenVNodeName(vnodes, dbName, totalCount)
	assert.Equal(t, true, ok)
	assert.Equal(t, "v_test_db_node0001", vnode)

	// returns empty string
	vnodes[vnode] = "vnode2"
	vnode, ok = GenVNodeName(vnodes, dbName, totalCount)
	assert.Equal(t, false, ok)
	assert.Equal(t, "", vnode)
}

func TestCopySlice(t *testing.T) {
	s1 := []string{"one", "two"}
	s2 := CopySlice(s1)
	assert.Equal(t, len(s2), len(s1))
	assert.Equal(t, s1[0], s2[0])
	assert.Equal(t, s1[1], s2[1])
	s2 = append(s2, "three")
	assert.NotEqual(t, len(s2), len(s1))
}

func TestCopyMap(t *testing.T) {
	s1 := map[string]string{
		"1": "one",
		"2": "two",
	}
	s2 := CopyMap(s1)
	assert.Equal(t, len(s2), len(s1))
	assert.Equal(t, s1["1"], s2["1"])
	assert.Equal(t, s1["2"], s2["2"])
	s2["3"] = "three"
	assert.NotEqual(t, len(s2), len(s1))
}

func TestValidateCommunalStorageLocation(t *testing.T) {
	// return error for an empty location
	err := ValidateCommunalStorageLocation("")
	assert.Error(t, err)

	// no error for a valid s3 location
	err = ValidateCommunalStorageLocation("s3://vertica-fleeting/k8s/revive_eon_5")
	assert.NoError(t, err)

	// no error for a valid local location
	err = ValidateCommunalStorageLocation("/communal/vert/k8s/revive_eon_5")
	assert.NoError(t, err)

	// return error for a non-absolute local path
	err = ValidateCommunalStorageLocation("~/test_folder/test")
	assert.Error(t, err)

	// return error for an invalid s3 location with ":"
	err = ValidateCommunalStorageLocation("s3:vertica-fleeting/k8s/revive_eon_5")
	assert.Error(t, err)

	// return error for an invalid s3 location with ":/"
	err = ValidateCommunalStorageLocation("s3:/vertica-fleeting/k8s/revive_eon_5")
	assert.Error(t, err)

	// return error for an invalid s3 location with ":///"
	err = ValidateCommunalStorageLocation("s3:///vertica-fleeting/k8s/revive_eon_5")
	assert.Error(t, err)

	// return error for an invalid s3 location with "//" as the path separator
	err = ValidateCommunalStorageLocation("s3://vertica-fleeting//k8s/revive_eon_5")
	assert.Error(t, err)

	// return error for an invalid s3 location with "///" as the path separator
	err = ValidateCommunalStorageLocation("s3://vertica-fleeting///k8s/revive_eon_5")
	assert.Error(t, err)
}

func TestIsEmptyOrValidTimeStr(t *testing.T) {
	const layout = "2006-01-02 15:04:05.000000"
	testTimeString := ""

	// positive cases
	_, err := IsEmptyOrValidTimeStr(layout, testTimeString)
	assert.NoError(t, err)

	testTimeString = "2023-05-02 14:10:31.038289"
	_, err = IsEmptyOrValidTimeStr(layout, testTimeString)
	assert.NoError(t, err)

	// negative case
	testTimeString = "invalid time"
	_, err = IsEmptyOrValidTimeStr(layout, testTimeString)
	assert.ErrorContains(t, err, "cannot parse")
}

func TestGetEnvInt(t *testing.T) {
	key := "TEST_ENV_INT"
	fallback := 123
	// positive case: environment variable exists and is a valid integer
	os.Setenv(key, "456")
	actual := GetEnvInt(key, fallback)
	assert.Equal(t, 456, actual)

	// negative case: environment variable does not exist
	os.Unsetenv(key)
	actual = GetEnvInt(key, fallback)
	assert.Equal(t, fallback, actual)

	// negative case: environment variable exists but is not a valid integer
	os.Setenv(key, "not_an_integer")
	actual = GetEnvInt(key, fallback)
	assert.Equal(t, fallback, actual)
}

func TestGetClusterName(t *testing.T) {
	cluster := GetClusterName("")
	assert.Equal(t, "main cluster", cluster)

	cluster = GetClusterName("sand1")
	assert.Equal(t, "sandbox sand1", cluster)
}
