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
    chown -R dbadmin:verticadba /opt/vertica/config/logrotate
    rm -rf /home/dbadmin/logrotate

    mkdir -p /opt/vertica/config/licensing
    cp -r /home/dbadmin/licensing/ce/* /opt/vertica/config/licensing
    chown -R dbadmin:verticadba /opt/vertica/config/licensing
    chmod -R 0755 /opt/vertica/config/licensing
}

start_cron
copy_config_files

echo "Vertica container is now running"

sudo /usr/sbin/sshd -D
