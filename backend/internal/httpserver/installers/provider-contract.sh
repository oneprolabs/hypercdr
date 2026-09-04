# Provider contract assembled into /install.sh by the control plane.
# Provider modules implement these hooks; the orchestrator is the only caller.
provider_prepare_dependencies() { "provider_${CLUSTER_TYPE//-/_}_prepare_dependencies"; }
provider_select_context() { "provider_${CLUSTER_TYPE//-/_}_select_context"; }
provider_align_kubectl_version() { "provider_${CLUSTER_TYPE//-/_}_align_kubectl_version"; }
provider_verify() { "provider_${CLUSTER_TYPE//-/_}_verify"; }
provider_prepare_platform_trust() { "provider_${CLUSTER_TYPE//-/_}_prepare_platform_trust"; }
provider_prepare_preflight() { "provider_${CLUSTER_TYPE//-/_}_prepare_preflight"; }
provider_run_preflight() { "provider_${CLUSTER_TYPE//-/_}_run_preflight"; }
provider_install_platform_trust() { "provider_${CLUSTER_TYPE//-/_}_install_platform_trust"; }
provider_install_backup_backend() { "provider_${CLUSTER_TYPE//-/_}_install_backup_backend"; }
provider_rollback_backup_backend() { "provider_${CLUSTER_TYPE//-/_}_rollback_backup_backend"; }
