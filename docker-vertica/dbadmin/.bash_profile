# .bash_profile

# Get the aliases and functions
if [ -f ~/.bashrc ]; then
        . ~/.bashrc
fi

# User specific environment and startup programs

PATH=$PATH:$HOME/.local/bin:$HOME/bin:/opt/vertica/bin

export PATH

export PATH
export VBR_BACKUP_STORAGE_ACCESS_KEY_ID=minioadmin
export VBR_BACKUP_STORAGE_SECRET_ACCESS_KEY=minioadmin
export VBR_BACKUP_STORAGE_ENDPOINT_URL=http://192.168.1.132:9000/

# add EON comm storage parameters
export VBR_COMMUNAL_STORAGE_ACCESS_KEY_ID=minioadmin
export VBR_COMMUNAL_STORAGE_SECRET_ACCESS_KEY=minioadmin
export VBR_COMMUNAL_STORAGE_ENDPOINT_URL=http://192.168.1.132:9000/

export LANG=en_US.UTF-8
export TZ=UTC
