# (c) Copyright [2021-2023] Open Text.
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

# This script is used to parse Prometheus formatted text and generating a JSON
# document with all of the metrics included. This file is then stored in the
# repo and can be used to figure out what metrics exist without having to setup
# a running instance.

import argparse
import json
import re
import os


def setup_for_metric(metrics, metric_name):
    if metric_name not in metrics:
        metrics[metric_name] = {"name": metric_name}


def include_metric(metric_name):
    ignore_metrics = ["certwatcher_", "rest_", "leader_"]
    for m in ignore_metrics:
        if metric_name.startswith(m):
            return False
    return True


def parse_metrics(ip_file):
    metric_help_re = re.compile(r"^# HELP ([\w]+) (.+)")
    metric_type_re = re.compile(r"^# TYPE ([\w]+) (\w+)")

    metrics = {}
    with open(ip_file, 'r') as f:
        for ln in f:
            help_match = metric_help_re.match(ln)
            if help_match and include_metric(help_match.group(1)):
                setup_for_metric(metrics, help_match.group(1))
                metrics[help_match.group(1)]["description"] = help_match.group(2)
            type_match = metric_type_re.match(ln)
            if type_match and include_metric(type_match.group(1)):
                setup_for_metric(metrics, type_match.group(1))
                metrics[type_match.group(1)]["type"] = type_match.group(2)

    # Convert dict to list then sort by name as we want a consistent order
    metrics = list(metrics.values())
    metrics.sort(key=lambda m: m['name'])
    # Pretty print the metrics in json so that it can be consumed by other
    # scripts
    j = {"generatedWith": os.path.basename(__file__),
         "info": "Recreate by extracting output in kuttl test " +
         "'kubectl test --test metrics-no-auth'",
         "metrics": metrics}
    print(json.dumps(j, sort_keys=True, indent=4))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        prog="parse_prometheus_metrics.py",
        description="construct a JSON document of all of the prometheus "
                    "metrics passed in")
    parser.add_argument(
        "filename",
        help="The name of a file with Prometheus metrics. Must be in the "
             "text-based Prometheus exposition format")
    args = parser.parse_args()
    parse_metrics(args.filename)
