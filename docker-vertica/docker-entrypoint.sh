#!/bin/bash
set -e

start_cron(){
    # daemonizes, no need for &
    sudo /usr/sbin/crond
}

# We copy back the files normally stored in /opt/vertica/config/.  We do this
# because we have a Persistent Volume that backs /opt/vertica/config, so
# it starts up empty and must be populated
copy_config_files() {
    cp -r /home/dbadmin/logrotate/* /opt/vertica/config/
    rm -rf /home/dbadmin/logrotate

    mkdir -p /opt/vertica/config/licensing
    cp -r /home/dbadmin/licensing/ce/* /opt/vertica/config/licensing
    chmod -R 0755 /opt/vertica/config/licensing
}

# Ensure all PV paths are owned by dbadmin.  This is done for some PVs that
# start with restrictive ownership.
ensure_path_is_owned_by_dbadmin() {
    # -z is to needed in case input arg is empty
    [ -z "$1" ] || [ "$(stat -c "%U" "$1")" == "dbadmin" ] || sudo chown -R dbadmin:verticadba "$1"
}

# SPILLY - pretty hacky, don't want to leave this in
setup_dbadmin_keytab() {
    if [ -f /etc/krb5.keytab ]
    then
        KEYTAB=/home/dbadmin/keytab/krb5.keytab
        mkdir -p $(dirname $KEYTAB)
        cp /etc/krb5.keytab $KEYTAB
        sudo chown dbadmin:verticadba $KEYTAB
        sudo chmod 0600 $KEYTAB
    fi
}

start_cron
ensure_path_is_owned_by_dbadmin /opt/vertica/config
ensure_path_is_owned_by_dbadmin /opt/vertica/log
ensure_path_is_owned_by_dbadmin $DATA_PATH
ensure_path_is_owned_by_dbadmin $DEPOT_PATH
copy_config_files
setup_dbadmin_keytab

echo "Vertica container is now running"

sudo /usr/sbin/sshd -D
