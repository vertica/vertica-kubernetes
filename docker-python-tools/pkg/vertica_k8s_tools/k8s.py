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

import base64
from kubernetes import client, config
import logging
import os
from typing import Optional, Tuple


class VerticaK8s:
    """
    A helper class that gets information about the running Vertica cluster

    :param namespace: The kubernetes namespace we are operating in
    :param release_name: Name of the release given to the Vertica deployment.
    """
    def __init__(self, namespace : str, release_name : str):
        self.namespace = namespace
        self.release_name = release_name
        self.log = logging.getLogger()

        # If KUBERNETES_PORT environment variable is set then we assume we are
        # running inside a pod and can setup cluster using the 'incluster' api.
        # Otherwise, we load kube config from the ~/.kube/config.
        self.incluster = os.getenv('KUBERNETES_PORT') is not None
        if self.incluster:
            config.load_incluster_config()
        else:
            config.load_kube_config()

        self.coreV1Api = client.CoreV1Api()
        self.v1Apps = client.AppsV1Api()

    def get_database_name(self) -> str:
        """
        Return the name of the database as defined in kubernetes

        :return: Database name
        """
        cm = self.coreV1Api.read_namespaced_config_map(
            name=self._get_obj_name(),
            namespace=self.namespace
        )
        return cm.data['database-name']

    def get_su_passwd(self) -> str:
        """
        Return the superuser password (if it exists)

        :return: Superuser password or None
        """
        secret_name, secret_key = self._get_su_secret_name()
        if secret_name:
            secret = self.coreV1Api.read_namespaced_secret(
                name=secret_name,
                namespace=self.namespace
            )
            enc_pw = secret.data[secret_key]
            return base64.b64decode(enc_pw)
        return None

    def get_cluster_ip(self) -> str:
        """
        Return the cluster IP for connectivity for external clients

        :return: Cluster IP
        """
        if not self.incluster:
            return '127.0.0.1'

        svcs = self.coreV1Api.list_namespaced_service(
            namespace=self.namespace,
            label_selector='vertica.com/svc-type=external'
        )
        if len(svcs.items) == 0:
            self.log.warn(svcs)
            raise RuntimeError("Could not find service")
        return svcs.items[0].spec.cluster_ip

    def _get_pod_info_volume(self) -> dict:
        """
        Get the info for the podinfo volume
        """
        sts = self.v1Apps.read_namespaced_stateful_set(
            name=self._get_obj_name_with_subcluster(),
            namespace=self.namespace
        )
        for v in sts.spec.template.spec.volumes:
            if v.name == 'podinfo' and v.projected is not None:
                return v.projected
        self.log.warn(sts.spec.template.spec.volumes)
        raise RuntimeError("Could not find podinfo volume")

    def _get_su_secret_name(self) -> Tuple[Optional[str], Optional[str]]:
        """
        Get the name of the secret that has the superuser password

        :return: Name and key of the secret or None if not setup
        """
        pod_info_vol = self._get_pod_info_volume()
        for p in pod_info_vol.sources:
            if p.secret:
                for i in p.secret.items:
                    if i.path and i.path == 'superuser-passwd':
                        return p.secret.name, i.key
        return None, None

    def _get_obj_name(self) -> str:
        return '{}-vertica'.format(self.release_name)

    def _get_obj_name_with_subcluster(self) -> str:
        return '{}-defaultsubcluster'.format(self._get_obj_name())