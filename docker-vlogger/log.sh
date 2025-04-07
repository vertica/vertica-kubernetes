#!/bin/bash
set -e

FN=$DBPATH/v_*_catalog/vertica.log

if [[ "$LOG_LEVEL" == "WARNING" ]]; then
   until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN | grep WARNING
elif [[ "$LOG_LEVEL" == "INFO" ]]; then
   until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN | grep INFO
else
   FN=$DBPATH/v_*_catalog/vertica.log; until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN
fi

