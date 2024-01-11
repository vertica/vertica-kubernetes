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


def parse_metrics(ip_file):
    metric_help_re = re.compile(r"^# HELP ([\w]+) (.+)")
    metric_type_re = re.compile(r"^# TYPE ([\w]+) (\w+)")

    metrics = {}
    with open(ip_file, 'r') as f:
        for ln in f:
            help_match = metric_help_re.match(ln)
            if help_match:
                setup_for_metric(metrics, help_match.group(1))
                metrics[help_match.group(1)]["description"] = help_match.group(2)
            type_match = metric_type_re.match(ln)
            if type_match:
                setup_for_metric(metrics, type_match.group(1))
                metrics[type_match.group(1)]["type"] = type_match.group(2)

    # Convert dict to list then sort by name as we want a consistent order
    metrics = list(metrics.values()) 
    metrics.sort(key=lambda m: m['name'])
    # Pretty print the metrics in json so that it can be consumed by other
    # scripts
    j = {"generatedWith": os.path.basename(__file__),
         "info": "Recreate using 'make http_test'",
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
