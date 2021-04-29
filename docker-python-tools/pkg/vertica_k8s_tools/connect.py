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

import vertica_python
from .k8s import VerticaK8s


def connect(namespace : str, release_name : str):
    """
    Establishes a connection to Vertica

    :param namespace: The kubernetes namespace we are operating in
    :param release_name: Name of the release given to the Vertica deployment.

    >>> import vertica_k8s_tools as vkt
    >>> with vkt.connect('default', 'cluster') as conn:
            cur = conn.cursor()
            cur.execute("CREATE TABLE T1 (C1 INT)")
    """
    return K8sVerticaConnection(namespace, release_name)


class K8sVerticaConnection:
    """
    Wrapper for vertica_python connection in a Kubernetes environment.

    :param namespace: The kubernetes namespace we are operating in
    :param release_name: Name of the release given to the Vertica deployment.
    """
    def __init__(self, namespace : str, release_name : str):
        self.v_k8s = VerticaK8s(namespace, release_name)
        self.vconn = self._get_connection()

    def __enter__(self):
        return self.vconn

    def __exit__(self, type_, value, traceback):
        if self.vconn:
            self.vconn.close()

    def _get_connection(self) -> vertica_python.Connection:
        """
        Open a connection to Vertica, returning vertica_python connection obj

        :return: Connection object
        """
        conn_info = self._build_conn_info()
        return vertica_python.connect(**conn_info)

    def _build_conn_info(self) -> dict:
        """
        Construct the connection info to authenticate with Vertica

        :return: connection info
        """
        conn_info = {
            'host': self.v_k8s.get_cluster_ip(),
            'user': 'dbadmin',
            'database': self.v_k8s.get_database_name()
        }

        # The superuser password is optional.
        su_passwd = self.v_k8s.get_su_passwd()
        if su_passwd:
            conn_info['password'] = su_passwd
        return conn_info