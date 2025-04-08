#!/bin/sh

# -------------------------------
# Example usage: LOG_LEVELS="INFO -DEBUG" ./tail_vertica_log.sh
# Prefix level with '-' to exclude.
# 
# $DBPATH is set by the operator and is the /<localDataPath>/<dbName>.
# The tail can't be done until the vertica.log is created.  This is because the
# exact location isn't known until the server pod has come up and is added to
# the cluster. 
# Note: we use the '-F' option with tail so that it survives log rotations.

# -------------------------------
# Configuration & Valid Log Levels
# -------------------------------

VALID_LEVELS="TRACE DEBUG INFO WARN WARNING ERROR FATAL CRITICAL"
LOG_FILE=$DBPATH/v_*_catalog/vertica.log # DBPATH is set by operator
RAW_LEVELS="${LOG_LEVELS:-$*}" # LOG_LEVELS is set through crd side cars env value

# -------------------------------
# Helper Functions
# -------------------------------

to_upper() {
  echo "$1" | tr '[:lower:]' '[:upper:]'
}

is_valid_level() {
  for lev in $VALID_LEVELS; do
    [ "$lev" = "$1" ] && return 1
  done
  return 0
}

# -------------------------------
# Parse log levels
# -------------------------------

for raw in $RAW_LEVELS; do
  EXCLUDE=0
  case "$raw" in
    -*)
      EXCLUDE=1
      raw=$(echo "$raw" | sed 's/^-//')
      ;;
  esac

  level=$(to_upper "$raw")
  is_valid_level "$level"
  if [ $? -ne 1 ]; then
    echo "Invalid log level found, skip: $level"
    echo "Valid levels: $VALID_LEVELS"
    continue
  fi

  if [ "$EXCLUDE" -eq 1 ]; then
    EXCLUDE_LEVELS="${EXCLUDE_LEVELS}${level}|"
  else
    INCLUDE_LEVELS="${INCLUDE_LEVELS}${level}|"
  fi
done

INCLUDE_PATTERN=$(echo "$INCLUDE_LEVELS" | sed 's/|$//') # e.g: Including log levels: INFO|WARNING
EXCLUDE_PATTERN=$(echo "$EXCLUDE_LEVELS" | sed 's/|$//') # e.g: Excluding log levels: ERROR|CRITICAL

# -------------------------------
# Tail and Filter log content with include and exclude pattern
# -------------------------------

print_logs() {
  tail -n 1 -F $LOG_FILE | while read -r line; do
    UPPER_LINE=$(to_upper "$line")
    INCLUDE_MATCH=1
    EXCLUDE_MATCH=0
    # Check if new line is in include pattern
    if [ -n "$INCLUDE_PATTERN" ]; then
      echo "$UPPER_LINE" | grep -qiE "$INCLUDE_PATTERN" || INCLUDE_MATCH=0
    fi
    # Check if new line is in exclude pattern
    if [ -n "$EXCLUDE_PATTERN" ]; then
      echo "$UPPER_LINE" | grep -qiE "$EXCLUDE_PATTERN" && EXCLUDE_MATCH=1
    fi

    if [ "$INCLUDE_MATCH" -eq 1 ] && [ "$EXCLUDE_MATCH" -eq 0 ]; then
      echo "$line"
    fi
  done
}

# -------------------------------
# Keep checking if log file available and read
# -------------------------------
until [ -f $LOG_FILE ]; do 
  sleep 5; 
done; 
print_logs
