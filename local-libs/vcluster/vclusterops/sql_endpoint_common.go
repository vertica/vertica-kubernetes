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

package vclusterops

import "fmt"

type sqlEndpointData struct {
	DBUsername string `json:"username"`
	DBPassword string `json:"password"`
	DBName     string `json:"dbname"`
}

func createSQLEndpointData(username, dbName string, useDBPassword bool, password *string) sqlEndpointData {
	sqlConnectionData := sqlEndpointData{}
	sqlConnectionData.DBUsername = username
	sqlConnectionData.DBName = dbName
	if useDBPassword {
		sqlConnectionData.DBPassword = *password
	}
	return sqlConnectionData
}

func ValidateSQLEndpointData(opName string, useDBPassword bool, userName string,
	password *string, dbName string) error {
	if userName == "" {
		return fmt.Errorf("[%s] should always provide a username for local database connection", opName)
	}
	if dbName == "" {
		return fmt.Errorf("[%s] should always provide a database name for local database connection", opName)
	}
	if useDBPassword && password == nil {
		return fmt.Errorf("[%s] should properly set the password when a password is configured", opName)
	}
	return nil
}
