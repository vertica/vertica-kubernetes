# (c) Copyright [2021] Micro Focus or one of its affiliates.
# Licensed under the Apache License, Version 2.0 (the "License");
# You may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This is a script for re-ip local Vertica node in Kubernetes Init Container
# and restart local Vertica node in Kubernetes Vertica app container
# the script is expected to be simple and lightweight
import sys
import subprocess
import os
import configparser
import argparse

def checkAdmintoolsConfExists(atConfFilePath):
    return os.path.isfile(atConfFilePath)

def getCommandResult(args, label):
    proc = subprocess.Popen(args, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, shell=True)
    result, error = proc.communicate()
    if proc.returncode != 0:
        print(f"Error in running command {label}: {error.decode(sys.stdout.encoding)}")
    return result.decode(sys.stdout.encoding)

def getActiveDB():
    activeDb = ""
    args = "/opt/vertica/bin/admintools -t show_active_db"
    activeDbStr = getCommandResult(args, "get active db").rstrip()
    if not activeDbStr:
        return ""
    # split output by " ", following the output format of show_active_db
    activeDb = activeDbStr.split(" ")[0]
    print(f"active database is: {activeDb}")
    return activeDb

def getLocalVerticaNodeName(dbName, atConfFilePath):
    localVNodeName = ""
    if atConfFilePath is None or not os.path.isfile(atConfFilePath) or os.stat(atConfFilePath).st_size <= 0:
        return False, localVNodeName

    atConfig = configparser.ConfigParser()
    try:
        atConfig.read(atConfFilePath)
    except configparser.Error as err:
        print(f"Error during reading admintools.conf: {err}")
        return False, localVNodeName

    if not atConfig.has_section('Nodes'):
        return False, localVNodeName

    # important assumption: all nodes have the same catalog path
    templateNodeInfo = atConfig.items('Nodes')[0]
    catalogPath = templateNodeInfo[1].split(",")[1]

    # get local vertica node name by checking catalog directory on local pod
    args = f"ls {catalogPath}/{dbName}/ | grep '^v_.*_catalog$' | head -1 | sed 's/_[^_]*$//'"

    localVNodeName = getCommandResult(args, "get local Vertica node name").rstrip()
    if not localVNodeName:
        return False, localVNodeName
    return True, localVNodeName

def getLocalPodIp():
    return os.environ['POD_IP']

def getDbAdminPwd():
    getPwdArgs = "cat /etc/podinfo/superuser-passwd 2> /dev/null || :"
    dbadminPwd = getCommandResult(getPwdArgs, "get dbadmin secret").rstrip()
    return dbadminPwd

def reIpLocalNodeClusterUp(activeDb, atConfFilePath, localVNodeName):
    localVNodeNewIp = getLocalPodIp()

    if not localVNodeNewIp:
        return False, "Failed to get IP address of local Pod."

    # get dbadmin password
    dbadminPwd = getDbAdminPwd()
    args = ""
    # call AT db_change_node_ip to re-ip the local Vertica node
    passOpts = " -p {}".format(dbadminPwd) if dbadminPwd else ""
    args = f"/opt/vertica/bin/admintools -t db_change_node_ip -d {activeDb} -s {localVNodeName} --new-host-ips {localVNodeNewIp}{passOpts}"

    print(f"Updating IP address of local Vertica node {localVNodeName} to be {localVNodeNewIp} ...")
    nodeReIpResult = getCommandResult(args, "update IP address of local Vertica node")

    # check if the node has been re-iped successfully
    nodeReIpMsg = "IP addresses of nodes have been updated successfully"
    nodeNoIpChangeMsg = "Skip updating IP addresses: all nodes have up-to-date addresses"
    if nodeReIpMsg in nodeReIpResult or nodeNoIpChangeMsg in nodeReIpResult:
        return True, ""
    else:
        return False, "Failed to update IP address for local Vertica node, check admintools.log for more information."

def restartLocalNode(activeDb, atConfFilePath, localVNodeName):
    # get dbadmin password
    dbadminPwd = getDbAdminPwd()
    args = ""
    # call AT restart_node to restart the local Vertica node
    passOpts = " -p {}".format(dbadminPwd) if dbadminPwd else ""
    args = f"/opt/vertica/bin/admintools -t restart_node -d {activeDb} -s {localVNodeName}{passOpts}"

    print(f"Restarting local Vertica node {localVNodeName} ...")
    restartResult = getCommandResult(args, "restart local Vertica node")

    # check if the node has been restarted successfully
    nodeRestartMsg = f"{localVNodeName}: (UP)"
    if nodeRestartMsg in restartResult:
        return True, ""
    else:
        return False, "Failed to restart local Vertica node, check admintools.log for more information."

def main():
    argParser = argparse.ArgumentParser()
    argParser.add_argument("--re-ip-node",
                           action = 'store_true',
                           dest = "reIpNode",
                           help = "Update IP address of local Vertica node")
    argParser.add_argument("--restart-node",
                           action = 'store_true',
                           dest = "restartNode",
                           help = "Restart local Vertica node")
    args = argParser.parse_args()
    atConfFilePath = "/opt/vertica/config/admintools.conf"
    # step 1: check if there is an admintools.conf, if no, then local host does not belong to a Vertica cluster
    atConfExists = checkAdmintoolsConfExists(atConfFilePath)
    if not atConfExists:
        print("The host does not belong to a Vertica cluster.")
        sys.exit(0)

    # step 2: check if there's an UP database, if not then we need to first bring up the database
    # we do not handle bringing up database in this script
    activeDb = getActiveDB()
    if activeDb == "":
        print("There is no UP database to restart the node, try start the database first.")
        sys.exit(0)

    # find local vertica node name
    (succeeded, localVNodeName) = getLocalVerticaNodeName(activeDb, atConfFilePath)
    if not succeeded:
        print("Local Vertica node does not belong to a Vertica database.")
        sys.exit(0)

    if args.reIpNode:
        # re-ip the local Vertica node. When a Pod dies, it could take a short while for the Vertica node in that Pod
        # to switch to status DOWN. Therefore, this step may fail due to that the Vertica node is not DOWN yet, but Init
        # Container will retry this script, so no retry needed in this script.
        (succeeded, msg) = reIpLocalNodeClusterUp(activeDb, atConfFilePath, localVNodeName)
        if not succeeded:
            print(f"Error in updating IP address for local Vertica node: {msg}")
            sys.exit(1)
        else:
            print("IP address of local Vertica node has been updated successfully.")
            sys.exit(0)
    elif args.restartNode:
        (succeeded, msg) = restartLocalNode(activeDb, atConfFilePath, localVNodeName)
        if not succeeded:
            print(f"Error in restarting local Vertica node: {msg}")
            sys.exit(1)
        else:
            print("Local Vertica node has been restarted successfully.")
            sys.exit(0)

if __name__ == '__main__':
    main()
