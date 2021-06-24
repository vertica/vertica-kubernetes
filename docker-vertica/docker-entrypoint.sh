#!/bin/bash
set -e

start_cron(){
    # daemonizes, no need for &
    sudo /usr/sbin/crond
}

# We copy back the logrotate files in their original location /opt/vertica/config/
# that's because we have a Persistent Volume that backs /opt/vertica/config, so
# it starts up empty and must be populated
copy_logrotate_files(){
    # We must use sudo in case the PV was created with permissions less than 0777.
    sudo cp -r /home/dbadmin/logrotate/* /opt/vertica/config/
    rm -rf /home/dbadmin/logrotate
}

start_cron
copy_logrotate_files

echo "Vertica container is now running"
sudo /usr/sbin/sshd -D