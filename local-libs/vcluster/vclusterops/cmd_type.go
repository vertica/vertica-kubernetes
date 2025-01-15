package vclusterops

type CmdType int

const (
	// command types
	CreateDBCmd CmdType = iota
	DropDBCmd
	StopDBCmd
	StartDBCmd
	AddNodeCmd
	RemoveNodeCmd
	StartNodeCmd
	StopNodeCmd
	RestartNodeCmd
	AddSubclusterCmd
	RemoveSubclusterCmd
	StopSubclusterCmd
	StartSubclusterCmd
	SandboxSCCmd
	UnsandboxSCCmd
	ShowRestorePointsCmd
	SaveRestorePointsCmd
	InstallPackagesCmd
	ConfigRecoverCmd
	GetDrainingStatusCmd
	ManageConnectionDrainingCmd
	SetConfigurationParameterCmd
	GetConfigurationParameterCmd
	ReplicationStartCmd
	PromoteSandboxToMainCmd
	FetchNodesDetailsCmd
	AlterSubclusterTypeCmd
	RenameScCmd
	ReIPCmd
	ScrutinizeCmd
	CreateDBSyncCat
	StartDBSyncCat
	StopDBSyncCat
	StopSCSyncCat
	AddNodeSyncCat
	StartNodeSyncCat
	RemoveNodeSyncCat
	CreateArchiveCmd
	PollSubclusterStateCmd
	UpgradeLicenseCmd
)

var cmdStringMap = map[CmdType]string{
	CreateDBCmd:                  "create_db",
	DropDBCmd:                    "drop_db",
	StopDBCmd:                    "stop_db",
	StartDBCmd:                   "start_db",
	AddNodeCmd:                   "add_node",
	RemoveNodeCmd:                "remove_node",
	StartNodeCmd:                 "start_node",
	StopNodeCmd:                  "stop_node",
	RestartNodeCmd:               "restart_node",
	AddSubclusterCmd:             "add_subcluster",
	RemoveSubclusterCmd:          "remove_subcluster",
	StopSubclusterCmd:            "stop_subcluster",
	StartSubclusterCmd:           "start_subcluster",
	SandboxSCCmd:                 "sandbox_subcluster",
	UnsandboxSCCmd:               "unsandbox_subcluster",
	ShowRestorePointsCmd:         "show_restore_points",
	SaveRestorePointsCmd:         "save_restore_point",
	InstallPackagesCmd:           "install_packages",
	ConfigRecoverCmd:             "manage_config_recover",
	GetDrainingStatusCmd:         "get_draining_status",
	ManageConnectionDrainingCmd:  "manage_connection_draining",
	SetConfigurationParameterCmd: "set_configuration_parameter",
	ReplicationStartCmd:          "replication_start",
	PromoteSandboxToMainCmd:      "promote_sandbox_to_main",
	FetchNodesDetailsCmd:         "fetch_nodes_details",
	AlterSubclusterTypeCmd:       "alter_subcluster_type",
	RenameScCmd:                  "rename_subcluster",
	ReIPCmd:                      "re_ip",
	ScrutinizeCmd:                "scrutinize",
	CreateDBSyncCat:              "create_db_sync_cat",
	StartDBSyncCat:               "start_db_sync_cat",
	StopDBSyncCat:                "stop_db_sync_cat",
	StopSCSyncCat:                "stop_sc_sync_cat",
	AddNodeSyncCat:               "add_node_sync_cat",
	StartNodeSyncCat:             "start_node_sync_cat",
	RemoveNodeSyncCat:            "remove_node_sync_cat",
	CreateArchiveCmd:             "create_archive",
	PollSubclusterStateCmd:       "poll_subcluster_state",
	UpgradeLicenseCmd:            "upgrade_license",
}

func (cmd CmdType) CmdString() string {
	if str, ok := cmdStringMap[cmd]; ok {
		return str
	}
	return "unknown_operation"
}
