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
    # We must use sudo in case the PV was created with permissions less than 0777.
    sudo cp -r /home/dbadmin/logrotate/* /opt/vertica/config/
    rm -rf /home/dbadmin/logrotate

    sudo mkdir -p /opt/vertica/config/licensing
    sudo cp -r /home/dbadmin/licensing/ce/* /opt/vertica/config/licensing
    sudo chown -R dbadmin:verticadba /opt/vertica/config/licensing
    sudo chmod -R 0755 /opt/vertica/config/licensing
}

start_cron
copy_config_files

echo "Vertica container is now running"
sudo /usr/sbin/sshd -D
