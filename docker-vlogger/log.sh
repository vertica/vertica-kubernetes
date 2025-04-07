#!/bin/bash
set -e

FN=$DBPATH/v_*_catalog/vertica.log

if [[ "$LOG_LEVEL" == "WARNING" ]]; then
   until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN | grep WARNING
elif [[ "$LOG_LEVEL" == "ERROR" ]]; then
   until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN | grep ERROR
else
   until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN
fi

