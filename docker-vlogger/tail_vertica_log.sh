#!/bin/sh

# -------------------------------
# Parameters: 
# LOG_LEVEL: Defines the minimum log severity level to print.
# LOG_FILTER: Comma-separated list (e.g., INFO,-DEBUG) to override LOG_LEVEL, supports includes/excludes
#  
# Example usage:
# No env setup: prints all logs
# 
# Minimal setup: show INFO and above(INFO,WARNING,ERROR)
# LOG_LEVEL=INFO ./tail_vertica_log.sh
# 
# Override log level filter. Show ERROR and above, but keep the DEBUG level(DEBUG,ERROR)
# LOG_LEVEL=ERROR LOG_FILTER="DEBUG" ./tail_vertica_log.sh
# 
# Show log DEBUG and above, exclude INFO. Put log Prefix level with '-' to exclude(DEBUG,WARNING,ERROR)
# LOG_LEVEL=DEBUG LOG_FILTER="-INFO" ./tail_vertica_log.sh
# 
# Fallback to CLI if env not set: ./tail_vertica_log.sh $LOG_LEVEL $LOG_FILTER
# e.g: ./tail_vertica_log.sh WARN "DEBUG,-TRACE"
# 
# $DBPATH is set by the operator and is the /<localDataPath>/<dbName>.
# The tail can't be done until the vertica.log is created.  This is because the
# exact location isn't known until the server pod has come up and is added to
# the cluster. 
# Note: we use the '-F' option with tail so that it survives log rotations.

# -------------------------------
# Configuration & Valid Log Levels
# -------------------------------

VALID_LEVELS="DEBUG INFO WARNING ERROR" 
LOG_LEVEL="${LOG_LEVEL:-$1}" # Defines the minimum log severity level to print.
LOG_FILTER="${LOG_FILTER:-$2}" # Comma-separated list (e.g., INFO,-DEBUG) to override LOG_LEVEL, supports includes/excludes
LOG_FILE=$DBPATH/v_*_catalog/vertica.log # Log file path. DBPATH is set by operator

# -------------------------------
# Helper Functions
# -------------------------------

# Convert string to upper case
to_upper() {
  echo "$1" | tr '[:lower:]' '[:upper:]'
}

# Check if a level is valid
is_invalid_level() {
  for lvl in $VALID_LEVELS; do [ "$1" = "$lvl" ] && return 0; done
  return 1
}

# Build inclusive log pattern from base level
build_from_base_level() {
  local base="$1" found=0 result=""
  for lvl in $VALID_LEVELS; do
    [ "$lvl" = "$base" ] && found=1
    [ $found -eq 1 ] && result="$result|<$lvl>"
  done
  echo "$result" | sed 's/^|//'
}

# -------------------------------
# Parse log level filter
# -------------------------------

# Parse LOG_FILTER string into include/exclude
parse_log_filter() {
  INCLUDE=""
  EXCLUDE=""
  IFS=','

  set -- $1
  for raw in "$@"; do
    case "$raw" in
      -*)
        lev=$(to_upper "$(echo "$raw" | sed 's/^-//')")
        ;;
      *)
        lev=$(to_upper "$raw")
        ;;
    esac

    if is_invalid_level "$lev"; then
      echo "Invalid log level found, skip: $is_valid_level"
      echo "Valid levels: $VALID_LEVELS"
      continue
    fi
  
    case "$raw" in
      -*)
        EXCLUDE="${EXCLUDE}<${lev}>|"
        ;;
      *)
        INCLUDE="${INCLUDE}<${lev}>|"
        ;;
    esac
  done
  
  # Clean trailing pipes
  INCLUDE_PATTERN=$(echo "$INCLUDE" | sed 's/|$//') # e.g: <INFO>|<WARNING>
  EXCLUDE_PATTERN=$(echo "$EXCLUDE" | sed 's/|$//') # e.g: <ERROR>|<WARNING>
}

# -------------------------------
# Determine log and filter pattern
# -------------------------------

if [ -n "$LOG_LEVEL" ]; then
  LEVEL=$(to_upper "$LOG_LEVEL")
  is_invalid_level "$LEVEL" || LEVEL="INFO"
  LEVEL_PATTERN=$(build_from_base_level "$LEVEL") # Provide all log levels after the base level
fi
if [ -n "$LOG_FILTER" ]; then
  parse_log_filter "$LOG_FILTER"
fi

# -------------------------------
# Tail and Filter log content with include and exclude pattern
# -------------------------------

print_logs() {
  tail -n 1 -F $LOG_FILE | while read -r line; do
    upper_line=$(to_upper "$line")
    show=1
    # Apply Log Level
    if [ -n "$LEVEL_PATTERN" ]; then # e.g: <INFO>|<WARNING>|<ERROR>
      echo "$upper_line" | grep -qiE "$LEVEL_PATTERN" || show=0
    fi

    # Apply INCLUDE filter, overwrite if needed
    if [ -n "$INCLUDE_PATTERN" ]; then
      echo "$upper_line" | grep -qiE "$INCLUDE_PATTERN" && show=1
    fi

    # Apply EXCLUDE filter, overwrite if needed
    if [ -n "$EXCLUDE_PATTERN" ]; then
      echo "$upper_line" | grep -qiE "$EXCLUDE_PATTERN" && show=0
    fi

    [ "$show" -eq 1 ] && echo "$line"
  done
}

# -------------------------------
# Keep checking if log file available and read
# -------------------------------
echo "Waiting for log file: $LOG_FILE"
until [ -f $LOG_FILE ]; do 
  sleep 5; 
done; 
echo "Log file Ready. Tailing..."
print_logs
