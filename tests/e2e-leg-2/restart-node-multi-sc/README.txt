Tests scenario found in VER-77530.  It test db_restart_node when the vnode and
compat21 nodes don't match.  This is done by using multiple subclusters and
removing some nodes between install and add node.
