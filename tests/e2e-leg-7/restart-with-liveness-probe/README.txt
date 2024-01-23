This does restart by letting the livenessProbe schedule the pod. It will invoke
that method by killing the vertica PID rather than the entire pod.
