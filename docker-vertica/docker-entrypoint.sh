#!/bin/bash
set -e

start_cron(){
    # daemonizes, no need for &
    sudo /usr/sbin/crond
}

# Kubernetes start-up is a little weird
#  - in order to configure the host-list correctly, k8s
#    has to do an install_vertica, which writes to
#    non-volatile store
#  - but the agent needs things that will be created by
#    that install.
#  - so we don't start the agent until we find the database running 
start_agent_when_ready(){
    agent_started=No
    while [ $agent_started == No ]; do
        if [ -f /opt/vertica/config/admintools.conf ]; then
            # safe to try to run admintools
            db=$(/opt/vertica/bin/admintools -t show_active_db) || true
            # If we ask too early --- before this container's database has been
            # created, the output is not a database name (nor is it empty), it
            # is an error message that begins "admintools cannot be run..."
            case "$db"x in
                x|*admintools*cannot*)
                    sleep 15
                    ;;
                *)
                    echo "Starting vertica agent for db $db"
                    sudo /opt/vertica/sbin/vertica_agent start \
                         2> /tmp/agent_start.err \
                         1> /tmp/agent_start.out
                    echo "Agent started"
                    agent_started=Yes
                    ;;
            esac
        else
            sleep 15
        fi
    done 
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
start_agent_when_ready &

echo "Vertica container is now running"
sudo /usr/sbin/sshd -D